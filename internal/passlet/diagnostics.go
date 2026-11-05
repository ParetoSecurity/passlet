package passlet

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"time"
)

// AccountInfo is stored with credentials in the protected Keychain item.
type AccountInfo struct {
	InstallationOwner string    `json:"installation_owner,omitempty"`
	Login             string    `json:"login"`
	AppID             int64     `json:"app_id"`
	AppURL            string    `json:"app_url"`
	VerifiedAt        time.Time `json:"verified_at"`
}

func (a AccountInfo) Validate() error {
	if !component.MatchString(a.Login) || a.AppID <= 0 || a.VerifiedAt.IsZero() || GitHubAppSettingsURL(a.AppURL) == "https://github.com/settings/apps" {
		return errors.New("invalid_protected_account_identity")
	}
	return nil
}

type healthConnectionError struct{ cause error }

func (e healthConnectionError) Error() string {
	return "approver_unavailable_check_service_or_SSH_tunnel"
}
func (e healthConnectionError) Unwrap() error { return e.cause }

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
		return healthConnectionError{cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
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
	if err != nil { // coverage-ignore: Fresh command; only process-wide file-descriptor exhaustion can fail pipe creation.
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

func ReadAudit(path string, limit int) ([]AuditEntry, int, error) {
	if limit < 1 || limit > 1000 || !filepath.IsAbs(path) {
		return nil, 0, errors.New("audit_requires_absolute_path_and_limit_1_to_1000")
	}
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil { // coverage-ignore: Registered SQLite driver opens lazily; unavailable databases fail the tested query path.
		return nil, 0, errors.New("audit_unavailable")
	}
	defer func() { _ = db.Close() }()
	rows, err := db.Query("SELECT at,data FROM events ORDER BY id DESC LIMIT ?", limit)
	if err != nil {
		return nil, 0, errors.New("audit_unavailable")
	}
	defer func() { _ = rows.Close() }()
	entries := []AuditEntry{}
	skipped := 0
	for rows.Next() {
		var e AuditEntry
		var data string
		if rows.Scan(&e.At, &data) != nil || json.Unmarshal([]byte(data), &e.Event) != nil {
			skipped++
			continue
		}
		entries = append(entries, e)
	}
	return entries, skipped, rows.Err()
}
