package passlet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// AccountInfo is informational, not an authorization source. It never contains
// credentials and status explicitly labels it as locally recorded metadata.
type AccountInfo struct {
	Login      string    `json:"login"`
	AppID      int64     `json:"app_id"`
	AppURL     string    `json:"app_url"`
	VerifiedAt time.Time `json:"verified_at"`
}

func SaveAccount(path string, account AccountInfo) error {
	if err := PrivateDir(filepath.Dir(path)); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".account-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := json.NewEncoder(f).Encode(account); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func LoadAccount(path string) (*AccountInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var account AccountInfo
	if json.NewDecoder(io.LimitReader(f, 8192)).Decode(&account) != nil || !component.MatchString(account.Login) || account.AppID <= 0 {
		return nil, errors.New("account_metadata_invalid")
	}
	return &account, nil
}

func Health(ctx context.Context, endpoint string) error {
	client, close, err := EndpointClient(endpoint)
	if err != nil {
		return err
	}
	defer close()
	ctx, cancel := context.WithTimeout(ctx, ConnectDeadline)
	defer cancel()
	target, _ := EndpointURL(endpoint, "/v1/health")
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	resp, err := client.Do(req)
	if err != nil {
		return errors.New("approver_unavailable_check_service_or_SSH_tunnel")
	}
	defer resp.Body.Close()
	var health struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&health) != nil || health.Status != "ready" {
		return errors.New("approver_health_check_failed")
	}
	return nil
}

func KeychainStatus(ctx context.Context, helper string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, ConnectDeadline)
	defer cancel()
	cmd := exec.CommandContext(ctx, helper, "--status")
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", errors.New("helper_unavailable")
	}
	if err := cmd.Start(); err != nil {
		return "", errors.New("helper_unavailable")
	}
	var result struct {
		Status string `json:"status"`
	}
	err = json.NewDecoder(io.LimitReader(pipe, 1024)).Decode(&result)
	waitErr := cmd.Wait()
	if err != nil || waitErr != nil {
		return "", errors.New("helper_status_unavailable")
	}
	switch result.Status {
	case "configured", "missing", "protected_or_unavailable":
		return result.Status, nil
	default:
		return "", errors.New("helper_status_invalid")
	}
}

type AuditEntry struct {
	At    string `json:"at"`
	Event Event  `json:"event"`
}

func ReadAudit(path string, limit int) ([]AuditEntry, error) {
	if limit < 1 || limit > 1000 || !filepath.IsAbs(path) {
		return nil, errors.New("audit_requires_absolute_path_and_limit_1_to_1000")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, errors.New("audit_unavailable")
	}
	defer db.Close()
	rows, err := db.Query("SELECT at,data FROM events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, errors.New("audit_unavailable")
	}
	defer rows.Close()
	entries := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var data string
		if rows.Scan(&e.At, &data) != nil || json.Unmarshal([]byte(data), &e.Event) != nil {
			return nil, errors.New("audit_invalid")
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
