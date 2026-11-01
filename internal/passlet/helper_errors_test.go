package passlet

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHelperStoreClosedPipe(t *testing.T) {
	in, err := os.CreateTemp(t.TempDir(), "input")
	if err != nil {
		t.Fatal(err)
	}
	in.Close()
	old := Credentials{AccessToken: "old"}
	s := &helperSession{in: in, credentials: old, scan: bufio.NewScanner(strings.NewReader(""))}
	if err := s.Store(Credentials{AccessToken: "new"}); err == nil || err.Error() != "helper_unavailable" {
		t.Fatalf("closed helper store: %v", err)
	}
	if s.Credentials() != old {
		t.Fatal("failed send modified credentials")
	}
}

func TestResetPropagatesDeletionFailureWithAnyIdentity(t *testing.T) {
	for _, validIdentity := range []bool{false, true} {
		c := Credentials{ClientID: "id", ClientSecret: "secret", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
		if validIdentity {
			c.Account = AccountInfo{Login: "alice", AppID: 1, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}
		}
		response, err := json.Marshal(helperReply{OK: true, Credentials: &c})
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "helper")
		script := "#!/bin/sh\nread -r request\nprintf '%s\\n' '" + string(response) + "'\nif read -r request; then printf '%s\\n' '{\"ok\":false,\"error\":\"keychain_unavailable\"}'; fi\n"
		if err := os.WriteFile(path, []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
		_, err = (NativeHelper{Path: path}).Reset(context.Background())
		want := "keychain_unavailable"
		if err == nil || err.Error() != want {
			t.Fatalf("reset: want %s, got %v", want, err)
		}
	}
}

func TestResetDeletesCredentialsWithMissingAccountInfo(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	script := `#!/bin/sh
read -r request
printf '%s\n' '{"ok":true,"credentials":{"client_id":"test-id","client_secret":"test-secret","access_token":"test-access","refresh_token":"test-refresh","expires_at":"2026-01-01T00:00:00Z"}}'
read -r request
if [ "$request" = '{"op":"delete"}' ]; then
  printf '%s\n' '{"ok":true}'
else
  printf '%s\n' '{"ok":false,"error":"unexpected_operation"}'
fi
`
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	account, err := (NativeHelper{Path: path}).Reset(context.Background())
	if err != nil || account != (AccountInfo{}) {
		t.Fatalf("reset with missing account: %v, %v", account, err)
	}
}

func TestImportRejectsInvalidCredentialsAndHelperFailures(t *testing.T) {
	valid := Credentials{ClientID: "client", ClientSecret: "secret", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour), Account: AccountInfo{Login: "alice", AppID: 1, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}}
	for _, tc := range []struct {
		name, script, want string
		invalidate         string
	}{
		{name: "invalid identity", invalidate: "identity", want: "invalid_protected_account_identity"},
		{name: "incomplete credentials", invalidate: "credentials", want: "incomplete_credentials"},
		{name: "missing helper", want: "helper_unavailable"},
		{name: "unencodable timestamp", invalidate: "timestamp", script: "exit 0", want: "helper_unavailable"},
		{name: "OS status", script: `printf '%s' '{"ok":false,"status":-25293}'`, want: "keychain_import_failed_osstatus_-25293"},
		{name: "malformed reply", script: `printf 'invalid'`, want: "keychain_import_failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := valid
			if tc.invalidate == "identity" {
				c.Account = AccountInfo{}
			}
			if tc.invalidate == "credentials" {
				c.AccessToken = ""
			}
			if tc.invalidate == "timestamp" {
				c.ExpiresAt = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
			}
			path := filepath.Join(t.TempDir(), "helper")
			if tc.script != "" {
				if err := os.WriteFile(path, []byte("#!/bin/sh\nread -r request\n"+tc.script+"\n"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := ImportCredentials(context.Background(), path, c); err == nil || err.Error() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
	if _, err := (NativeHelper{Path: filepath.Join(t.TempDir(), "missing")}).Approve(context.Background(), "prompt"); err == nil || err.Error() != "helper_unavailable" {
		t.Fatalf("missing helper: %v", err)
	}
}
