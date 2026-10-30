package passlet

import (
	"database/sql"
	"encoding/json"
	"errors"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
	"time"
)

type Event struct {
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
	f.Close()
	if err = os.Chmod(path, 0600); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000; PRAGMA synchronous=FULL; CREATE TABLE IF NOT EXISTS events (id INTEGER PRIMARY KEY, at TEXT NOT NULL, request_id TEXT NOT NULL, outcome TEXT NOT NULL, data TEXT NOT NULL);`)
	if err != nil {
		db.Close()
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
