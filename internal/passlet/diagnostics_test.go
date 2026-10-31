package passlet

import (
	"context"
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDiagnosticsDoNotApprove(t *testing.T) {
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
	if _, err := ReadAudit(path, 20); err == nil {
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
	entries, err := ReadAudit(path, 1)
	if err != nil || len(entries) != 1 || entries[0].Event.RequestID != "two" {
		t.Fatal("incorrect recent audit", err)
	}
	if _, err := ReadAudit(path, 1001); err == nil {
		t.Fatal("unbounded audit query")
	}
}

func TestAccountMetadataAndLaunchAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "private", "account.json")
	want := AccountInfo{Login: "alice", AppID: 123, VerifiedAt: time.Now().UTC()}
	if err := SaveAccount(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadAccount(path)
	if err != nil || got.Login != want.Login {
		t.Fatal(err)
	}
	data, err := LaunchAgent("/Applications/Passlet & Test.app/passlet", "/Applications/Passlet & Test.app/helper", "/Users/alice/.local/share/passlet/s", "/Users/alice/audit", "/Users/alice/log", "", "/usr/bin:/bin")
	if err != nil {
		t.Fatal(err)
	}
	d := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		_, err := d.Token()
		if err != nil {
			if err.Error() != "EOF" {
				t.Fatal(err)
			}
			break
		}
	}
	if strings.Contains(string(data), "--tailscale-listen") || !strings.Contains(string(data), "&amp;") || !strings.Contains(string(data), "SuccessfulExit") || !strings.Contains(string(data), "Aqua") {
		t.Fatal("unsafe launch agent")
	}
}
