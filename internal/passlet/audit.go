//go:build !agentonly

package passlet

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
	Operation           string            `json:"operation,omitempty"`
	GrantedOrganization string            `json:"granted_organization,omitempty"`
	GrantedAccount      string            `json:"granted_account,omitempty"`
	RequestID           string            `json:"request_id"`
	Caller              Caller            `json:"caller"`
	Request             Request           `json:"requested"`
	Outcome             string            `json:"outcome"`
	Decision            string            `json:"approval_decision,omitempty"`
	Approver            string            `json:"approver"`
	GrantedPermissions  map[string]string `json:"granted_permissions,omitempty"`
	GrantedRepositories []string          `json:"granted_repositories,omitempty"`
	ExpiresAt           *time.Time        `json:"expires_at,omitempty"`
	GitHubUser          string            `json:"github_user,omitempty"`
}
type Audit struct{ db *sql.DB }

func OpenAudit(path string) (*Audit, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, errors.New("audit_path_not_regular")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	_ = f.Close()
	if err = os.Chmod(path, 0600); err != nil { // coverage-ignore: Requires external removal or permission changes after successfully opening the file.
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil { // coverage-ignore: Registered SQLite driver opens lazily; real storage failures occur at Exec and are tested.
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, at TEXT NOT NULL, request_id TEXT NOT NULL, outcome TEXT NOT NULL, data TEXT NOT NULL);`)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Audit{db}, nil
}
func (a *Audit) Record(e Event) error {
	// Event deliberately has no token, credential, raw provider body, or free-text error field.
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = a.db.Exec("INSERT INTO events(at,request_id,outcome,data) VALUES(?,?,?,?)", time.Now().UTC().Format(time.RFC3339Nano), e.RequestID, e.Outcome, string(data))
	return err
}
func (a *Audit) Close() error { return a.db.Close() }

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
