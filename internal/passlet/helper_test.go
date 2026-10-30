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

func TestHelperProcess(t *testing.T) {
	mode := os.Getenv("PASSLET_HELPER_FIXTURE")
	if mode == "" {
		return
	}
	scan := bufio.NewScanner(os.Stdin)
	if !scan.Scan() {
		os.Exit(1)
	}
	switch mode {
	case "wait":
		for scan.Scan() {
		}
		os.Exit(0)
	case "deny":
		json.NewEncoder(os.Stdout).Encode(helperReply{Error: "approval_denied"})
		os.Exit(0)
	case "leak":
		json.NewEncoder(os.Stdout).Encode(helperReply{Error: "secret-parent-value"})
		os.Exit(0)
	case "oversize":
		os.Stdout.WriteString(strings.Repeat("x", 70000))
		os.Exit(0)
	}
	c := Credentials{ClientID: "client", ClientSecret: "secret-client", AccessToken: "secret-parent", RefreshToken: "secret-refresh", ExpiresAt: time.Now().Add(time.Hour)}
	json.NewEncoder(os.Stdout).Encode(helperReply{OK: true, Credentials: &c})
	if scan.Scan() {
		var req struct {
			Op          string      `json:"op"`
			Credentials Credentials `json:"credentials"`
		}
		if json.Unmarshal(scan.Bytes(), &req) != nil || req.Op != "store" || req.Credentials.RefreshToken != "rotated" {
			os.Exit(2)
		}
		json.NewEncoder(os.Stdout).Encode(helperReply{OK: true})
	}
	for scan.Scan() {
	}
	os.Exit(0)
}
func TestNativeHelperIPC(t *testing.T) {
	self, _ := os.Executable()
	for _, mode := range []string{"success", "wait", "deny", "leak", "oversize"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "helper")
			script := "#!/bin/sh\nGORACE=atexit_sleep_ms=0 PASSLET_HELPER_FIXTURE=" + mode + " exec '" + strings.ReplaceAll(self, "'", "'\\''") + "' -test.run=TestHelperProcess\n"
			if err := os.WriteFile(path, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			deadline := 5 * time.Second
			if mode == "wait" {
				deadline = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			start := time.Now()
			session, err := (NativeHelper{Path: path}).Approve(ctx, "grant test access")
			if mode == "success" {
				if err != nil {
					t.Fatal(err)
				}
				c := session.Credentials()
				c.RefreshToken = "rotated"
				if err = session.Store(c); err != nil {
					t.Fatal(err)
				}
				session.Close()
				session.Close()
			} else {
				if err == nil {
					session.Close()
					t.Fatal("unexpected success")
				}
				if strings.Contains(err.Error(), "secret") {
					t.Fatal("helper error leaked secret")
				}
			}
			if time.Since(start) > 2*time.Second {
				t.Fatal("helper cancellation did not terminate promptly")
			}
		})
	}
}
