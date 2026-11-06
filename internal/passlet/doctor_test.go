package passlet

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDoctorClientFailures(t *testing.T) {
	for _, endpoint := range []string{"http://invalid", "unix://" + socketPath(t)} {
		if _, err := Doctor(context.Background(), endpoint); err == nil {
			t.Fatal("unavailable endpoint passed")
		}
	}
	var missingContext context.Context
	if _, err := Doctor(missingContext, "unix://"+socketPath(t)); err == nil {
		t.Fatal("missing context accepted")
	}
	for _, tc := range []struct {
		name, body, want string
		status           int
	}{
		{"invalid JSON", `oops`, "invalid_doctor_response", 200},
		{"missing identity", `{}`, "invalid_doctor_response", 200},
		{"old approver", `{"error":"not_found"}`, "doctor_requires_updated_approver", 404},
		{"untrusted error", `{"error":"raw-secret"}`, "doctor_failed", 500},
		{"approval denied", `{"error":"approval_denied"}`, "approval_denied", 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := socketPath(t)
			listener, err := ListenUnix(path)
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})}
			listener = pairedFixture(t, server, listener)
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			if _, err := Doctor(context.Background(), "unix://"+path); err == nil || err.Error() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestDoctorRejectsUnverifiedIdentity(t *testing.T) {
	for _, invalidAccount := range []bool{true, false} {
		t.Run(fmt.Sprint("invalid account=", invalidAccount), func(t *testing.T) {
			account := AccountInfo{Login: "alice", AppID: 1, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}
			want := "github_identity_verification_failed"
			if invalidAccount {
				account = AccountInfo{}
				want = "invalid_protected_account_identity"
			}
			session := &testSession{c: Credentials{AccessToken: "parent-secret", Account: account}}
			provider := &testMinter{identityErr: errors.New("wrong account")}
			audit := &memoryAudit{}
			server := testServer(&testApprover{session: session}, provider, audit)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, httptest.NewRequest("POST", "/v1/doctor", strings.NewReader("{}")))
			if w.Code != 502 || w.Body.String() != "{\"error\":\""+want+"\"}\n" {
				t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
			}
			if provider.calls != 0 || !session.closed || session.stores != 0 {
				t.Fatal("failed verification must close credentials without minting or saving")
			}
			if audit.events[len(audit.events)-1].Outcome != want {
				t.Fatal("verification failure was not audited")
			}
		})
	}
}

func TestDoctorUsesApprovalGateAndAuditWithoutMinting(t *testing.T) {
	session := &testSession{c: Credentials{AccessToken: "parent-secret", Account: AccountInfo{Login: "alice", AppID: 1, AppURL: "https://github.com/apps/passlet-alice", VerifiedAt: time.Now()}}}
	approval := &testApprover{session: session}
	provider := &testMinter{refresh: true}
	audit := &memoryAudit{}
	server := testServer(approval, provider, audit)
	server.Identity = func(context.Context, string) (Caller, error) {
		return Caller{Transport: "tailscale", Name: "cruncher.tailnet.ts.net.", NodeID: "node"}, nil
	}
	request := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		server.ServeHTTP(w, httptest.NewRequest("POST", "/v1/doctor", strings.NewReader(body)))
		return w
	}
	if w := request(validBody); w.Code != 400 {
		t.Fatal("doctor accepted grant scope")
	}
	server.busy <- struct{}{}
	if w := request("{}"); w.Code != 409 {
		t.Fatal("doctor bypassed approval gate")
	}
	<-server.busy
	w := request("{}")
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"login":"alice"`) || strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
	}
	if provider.calls != 0 || session.stores != 1 || !session.closed || len(approval.prompts) != 1 {
		t.Fatal("doctor did not use the approved rotation flow")
	}
	if approval.prompts[0] != "verify stored credentials" {
		t.Fatal("unexpected diagnostic approval prompt")
	}
	verified := false
	for _, event := range audit.events {
		if event.Operation != "doctor" {
			t.Fatal("diagnostic audit not identified")
		}
		if event.Outcome == "verified" {
			verified = true
		}
	}
	if !verified {
		t.Fatal("verification not audited")
	}
}
