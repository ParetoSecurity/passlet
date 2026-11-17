//go:build !agentonly

package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

type statusTerminal struct {
	bytes.Buffer
	closed bool
	fail   bool
}

func (s *statusTerminal) Close() error { s.closed = true; return nil }
func (s *statusTerminal) Write(p []byte) (int, error) {
	if s.fail {
		return 0, errors.New("terminal disconnected")
	}
	return s.Buffer.Write(p)
}

func TestApprovalStatusLifecycle(t *testing.T) {
	for _, outcome := range []string{"approved", "approval_denied", "approval_cancelled", "approval_timeout", "approver_busy", "invalid_response", "cancelled", "unavailable", "no terminal", "disconnected terminal"} {
		t.Run(outcome, func(t *testing.T) {
			dir, err := os.MkdirTemp("/tmp", "pl-status-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(dir)
			t.Setenv("HOME", dir)
			listener, err := net.Listen("unix", filepath.Join(dir, "socket"))
			if err != nil {
				t.Fatal(err)
			}
			terminal := &statusTerminal{fail: outcome == "disconnected terminal"}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var requested passlet.Request
				if err := json.NewDecoder(r.Body).Decode(&requested); err != nil {
					t.Error(err)
					return
				}
				switch outcome {
				case "approved", "no terminal", "disconnected terminal":
					json.NewEncoder(w).Encode(passlet.Grant{ID: "fixture", Token: "fixture-secret", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Repositories: requested.Repositories, Permissions: requested.Permissions})
				case "cancelled":
					cancel()
				case "invalid_response":
					fmt.Fprint(w, "not JSON")
				default:
					w.WriteHeader(http.StatusForbidden)
					fmt.Fprintf(w, `{"error":%q}`, outcome)
				}
			})}
			listener = pairedFixture(t, server, listener)
			go server.Serve(listener)
			defer server.Close()
			if outcome == "unavailable" {
				server.Close()
			}
			requested := passlet.Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}
			var grant passlet.Grant
			if outcome == "no terminal" {
				grant, err = requestGrantWithStatus(ctx, "unix://"+filepath.Join(dir, "socket"), requested, nil)
			} else {
				grant, err = requestGrantWithStatus(ctx, "unix://"+filepath.Join(dir, "socket"), requested, terminal)
				if !terminal.closed {
					t.Fatal("terminal left open")
				}
			}
			approved := outcome == "approved" || outcome == "no terminal" || outcome == "disconnected terminal"
			if approved != (err == nil) || approved && grant.Token != "fixture-secret" {
				t.Fatalf("grant outcome: %v", err)
			}
			want := ""
			if outcome != "no terminal" && !terminal.fail {
				id := fmt.Sprintf("passlet-%d", os.Getpid())
				message := base64.StdEncoding.EncodeToString([]byte("Waiting for approval: alice/repo (contents:read)"))
				want = fmt.Sprintf("\x1b]7501;id=%s:state=blocked:kind=permission:app=passlet:msg=%s\x1b\\\x1b]7501;id=%s:state=clear\x1b\\", id, message, id)
			}
			if terminal.String() != want {
				t.Fatalf("status lifecycle: %q; want %q", terminal.String(), want)
			}
		})
	}
}

func TestApprovalStatusRejectsInvalidScope(t *testing.T) {
	terminal := &statusTerminal{}
	_, err := requestGrantWithStatus(context.Background(), "unused", passlet.Request{Repositories: []string{"alice/repo\x1b"}, Permissions: map[string]string{"contents": "read"}}, terminal)
	if err == nil || terminal.Len() != 0 || !terminal.closed {
		t.Fatalf("invalid scope emitted status: %q, %v", terminal.String(), err)
	}
}

func TestApprovalMessageScopes(t *testing.T) {
	for _, tc := range []struct {
		request passlet.Request
		target  string
	}{
		{passlet.Request{Repositories: []string{"alice/first", "alice/second"}}, "alice/first and 1 more"},
		{passlet.Request{Organization: "acme"}, "organization acme"},
		{passlet.Request{Account: "alice"}, "account alice"},
	} {
		tc.request.Permissions = map[string]string{"pull_requests": "write", "contents": "read", "metadata": "read"}
		want := "Waiting for approval: " + tc.target + " (contents:read, pull_requests:write)"
		if got := approvalMessage(tc.request); got != want {
			t.Fatalf("got %q; want %q", got, want)
		}
	}
	request := passlet.Request{Permissions: map[string]string{"contents": "read"}}
	for i := range 100 {
		request.Repositories = append(request.Repositories, "alice/"+strings.Repeat("r", 96)+fmt.Sprint(i))
	}
	if len(approvalMessage(request)) > 2048 {
		t.Fatal("status exceeds the protocol's message limit")
	}
}
