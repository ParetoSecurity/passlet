package passlet

import (
	"bytes"
	"context"
	"errors"
	"log"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestServerRejectsUnsupportedRequests(t *testing.T) {
	for _, tc := range []struct {
		method, path, origin string
		status               int
	}{
		{"POST", "/v1/grants", "https://example.com", 403},
		{"GET", "/missing", "", 404},
		{"GET", "/v1/grants", "", 405},
	} {
		s := testServer(&testApprover{}, &testMinter{}, &memoryAudit{})
		r := httptest.NewRequest(tc.method, tc.path, nil)
		r.Header.Set("Origin", tc.origin)
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d", tc.method, tc.path, w.Code)
		}
	}
	s := NewServer(&testApprover{}, &testMinter{}, &memoryAudit{})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/v1/health", nil))
	if w.Code != 403 {
		t.Fatalf("unauthenticated health: %d", w.Code)
	}
}

type minterFuncs struct {
	refresh func(context.Context, Credentials) (Credentials, error)
	mint    func(context.Context, Credentials, Request) (Grant, error)
}

func (m minterFuncs) Refresh(c context.Context, v Credentials) (Credentials, error) {
	return m.refresh(c, v)
}
func (m minterFuncs) Mint(c context.Context, v Credentials, r Request) (Grant, error) {
	return m.mint(c, v, r)
}

func TestServerProviderFailuresAndLateCancellation(t *testing.T) {
	for _, stage := range []string{"before refresh", "refresh", "mint", "after mint"} {
		t.Run(stage, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session := &testSession{}
			audit := &memoryAudit{}
			s := testServer(&testApprover{session: session}, &testMinter{}, audit)
			mints := 0
			s.GitHub = minterFuncs{
				refresh: func(_ context.Context, c Credentials) (Credentials, error) {
					if stage == "refresh" {
						return c, errors.New("reauthorization_required")
					}
					return c, nil
				},
				mint: func(_ context.Context, _ Credentials, r Request) (Grant, error) {
					mints++
					if stage == "mint" {
						return Grant{}, errors.New("repository_not_installed")
					}
					cancel()
					return Grant{Token: "must-not-deliver"}, nil
				},
			}
			if stage == "before refresh" {
				cancel()
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)).WithContext(ctx))
			want := 502
			if stage == "before refresh" || stage == "after mint" {
				want = 499
			}
			if w.Code != want || !session.closed || strings.Contains(w.Body.String(), "must-not-deliver") {
				t.Fatalf("response=%d closed=%t body=%s", w.Code, session.closed, w.Body.String())
			}
			if (stage == "refresh" || stage == "before refresh") && mints != 0 {
				t.Fatal("minted despite failed precondition")
			}
			if len(s.busy) != 0 {
				t.Fatal("approval slot not released")
			}
		})
	}
}

func TestAuditDeliveryFailureIsLogged(t *testing.T) {
	var output bytes.Buffer
	old := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(old)
	s := testServer(&testApprover{session: &testSession{}}, &testMinter{}, &failingAudit{n: 4})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if w.Code != 200 || !strings.Contains(output.String(), "audit_delivery_write_failed request_id=") || strings.Contains(output.String(), "secret") {
		t.Fatalf("delivery audit failure log: %s", output.String())
	}
}

func (g minterFuncs) VerifyIdentity(context.Context, Credentials, string) error { return nil }
