package passlet

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAuditStorageFailures(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "file")
	if err := os.WriteFile(file, []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(file, "audit.sqlite"), dir, file} {
		if audit, err := OpenAudit(path); err == nil {
			audit.Close()
			t.Fatalf("invalid audit storage accepted: %s", path)
		}
	}
	audit, err := OpenAudit(filepath.Join(dir, "valid.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer audit.Close()
	invalidTime := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	if err := audit.Record(Event{ExpiresAt: &invalidTime}); err == nil {
		t.Fatal("invalid timestamp stored")
	}
	if err := audit.Close(); err != nil {
		t.Fatal(err)
	}
	if err := audit.Record(Event{Outcome: "approved"}); err == nil {
		t.Fatal("closed audit accepted record")
	}
}

func TestAuditRejectsReadOnlyFile(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses file permissions")
	}
	path := filepath.Join(t.TempDir(), "readonly.sqlite")
	if err := os.WriteFile(path, nil, 0400); err != nil {
		t.Fatal(err)
	}
	if audit, err := OpenAudit(path); err == nil {
		audit.Close()
		t.Fatal("opened read-only audit for writing")
	}
}
