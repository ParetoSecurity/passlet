package passlet

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestHealthRejectsUnavailableAndInvalidEndpoints(t *testing.T) {
	for _, endpoint := range []string{"http://invalid", "unix://" + socketPath(t)} {
		if err := Health(context.Background(), endpoint); err == nil {
			t.Fatalf("reported healthy: %s", endpoint)
		}
	}
	l, err := ListenUnix(socketPath(t))
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"not ready"}`)) }))
	server.Listener.Close()
	server.Listener = l
	server.Start()
	defer server.Close()
	if err := Health(context.Background(), "unix://"+l.Addr().String()); err == nil || err.Error() != "approver_health_check_failed" {
		t.Fatalf("invalid health response: %v", err)
	}
}

func TestKeychainStatusFailures(t *testing.T) {
	for _, tc := range []struct{ name, script, want string }{
		{"missing helper", "", "helper_unavailable"},
		{"invalid JSON", "printf 'not json'", "helper_status_unavailable"},
		{"failed helper", "exit 1", "helper_status_unavailable"},
		{"unknown status", `printf '%s' '{"status":"unexpected"}'`, "helper_status_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper")
			if tc.script != "" {
				if err := os.WriteFile(path, []byte("#!/bin/sh\n"+tc.script+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := KeychainStatus(context.Background(), path); err == nil || err.Error() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestAuditSkipsCorruptRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.sqlite")
	a, err := OpenAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if err := a.Record(Event{RequestID: "older", Outcome: "approved"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.db.Exec(`INSERT INTO events(at,request_id,outcome,data) VALUES('now','one','approved','not json')`); err != nil {
		t.Fatal(err)
	}
	if err := a.Record(Event{RequestID: "newer", Outcome: "denied"}); err != nil {
		t.Fatal(err)
	}
	entries, skipped, err := ReadAudit(path, 20)
	if err != nil || skipped != 1 || len(entries) != 2 || entries[0].Event.RequestID != "newer" || entries[1].Event.RequestID != "older" {
		t.Fatalf("valid records around corruption lost: %+v, %v", entries, err)
	}
	// The limit still bounds the recent rows inspected, including corrupt rows.
	entries, skipped, err = ReadAudit(path, 2)
	if err != nil || skipped != 1 || len(entries) != 1 || entries[0].Event.RequestID != "newer" {
		t.Fatalf("recent-row limit ignored: %+v, %v", entries, err)
	}
	if _, err := a.db.Exec(`DELETE FROM events WHERE data != 'not json'`); err != nil {
		t.Fatal(err)
	}
	entries, skipped, err = ReadAudit(path, 20)
	if err != nil || skipped != 1 || entries == nil || len(entries) != 0 {
		t.Fatalf("all-corrupt log should produce an empty list: %+v, %v", entries, err)
	}
}

func TestHealthDoesNotApprove(t *testing.T) {
	s := NewServer(&testApprover{err: context.Canceled}, &testMinter{}, &memoryAudit{})
	endpoint := unixServer(t, s)
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	select {
	case s.busy <- struct{}{}:
		<-s.busy
	default:
		t.Fatal("health entered approval gate")
	}
}

func TestAuditReadIsReadOnlyAndBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit.sqlite")
	if _, _, err := ReadAudit(path, 20); err == nil {
		t.Fatal("missing audit accepted")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("read created database")
	}
	a, err := OpenAudit(path)
	if err != nil {
		t.Fatal(err)
	}
	a.Record(Event{RequestID: "one", Outcome: "approved"})
	a.Record(Event{RequestID: "two", Outcome: "delivery_failed"})
	a.Close()
	entries, skipped, err := ReadAudit(path, 1)
	if err != nil || skipped != 0 || len(entries) != 1 || entries[0].Event.RequestID != "two" {
		t.Fatal("incorrect recent audit", err)
	}
	if _, _, err := ReadAudit(path, 1001); err == nil {
		t.Fatal("unbounded audit query")
	}
}

func TestProtectedAccountIdentity(t *testing.T) {
	account := AccountInfo{Login: "alice", AppID: 123, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}
	if err := account.Validate(); err != nil {
		t.Fatal(err)
	}
	account.AppURL = "https://attacker.invalid/apps/passlet-alice"
	if account.Validate() == nil {
		t.Fatal("accepted invalid app identity")
	}
}
