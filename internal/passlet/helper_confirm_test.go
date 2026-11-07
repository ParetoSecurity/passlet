package passlet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNativeConfirm(t *testing.T) {
	for _, reply := range []string{`{"ok":true}`, `{"ok":false}`, `invalid`, ``} {
		t.Run(reply, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "helper")
			capture := filepath.Join(dir, "request")
			script := "#!/bin/sh\nIFS= read -r request\nprintf '%s' \"$request\" > '" + capture + "'\nprintf '%s\\n' '" + reply + "'\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			err := (NativeHelper{Path: path}).Confirm(context.Background(), "pair Agent alice@vm.")
			if (err == nil) != (reply == `{"ok":true}`) {
				t.Fatal(err)
			}
			data, _ := os.ReadFile(capture)
			if !strings.Contains(string(data), `"op":"confirm"`) || strings.Contains(string(data), "credentials") {
				t.Fatal(string(data))
			}
		})
	}
	if err := (NativeHelper{Path: "/bin/true"}).Confirm(context.Background(), "pair"); err == nil {
		t.Fatal("empty helper accepted")
	}
	if err := (NativeHelper{Path: "/missing/helper"}).Confirm(context.Background(), "pair"); err == nil {
		t.Fatal("missing helper accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (NativeHelper{Path: "/bin/cat"}).Confirm(ctx, "pair"); err == nil {
		t.Fatal("cancelled confirmation accepted")
	}
}

func TestConfirmHelperClosesWithoutReply(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	os.WriteFile(path, []byte("#!/bin/sh\nIFS= read -r request\nexit 0\n"), 0700)
	if err := (NativeHelper{Path: path}).Confirm(context.Background(), "pair"); err == nil {
		t.Fatal("EOF accepted")
	}
}
