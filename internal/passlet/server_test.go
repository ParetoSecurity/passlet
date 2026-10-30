package passlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type testSession struct {
	c        Credentials
	closed   bool
	stores   int
	storeErr error
}

func (s *testSession) Credentials() Credentials { return s.c }
func (s *testSession) Store(c Credentials) error {
	if s.storeErr != nil {
		return s.storeErr
	}
	s.c = c
	s.stores++
	return nil
}
func (s *testSession) Close() { s.closed = true }

type testApprover struct {
	session *testSession
	err     error
	entered chan struct{}
	wait    bool
}

func (a *testApprover) Approve(ctx context.Context, prompt string) (CredentialSession, error) {
	if a.entered != nil {
		select {
		case a.entered <- struct{}{}:
		default:
		}
	}
	if a.wait {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if a.err != nil {
		return nil, a.err
	}
	return a.session, nil
}

type testMinter struct {
	calls   int
	refresh bool
}

func (g *testMinter) Refresh(ctx context.Context, c Credentials) (Credentials, error) {
	if g.refresh {
		c.RefreshToken = "rotated-secret"
	}
	return c, nil
}
func (g *testMinter) Mint(ctx context.Context, c Credentials, r Request) (Grant, error) {
	g.calls++
	return Grant{Token: "secret-never-audited", ExpiresAt: time.Now().Add(8 * time.Hour), Repositories: r.Repositories, Permissions: r.Permissions, User: "zupo"}, nil
}

type memoryAudit struct {
	mu     sync.Mutex
	events []Event
	fail   bool
}

func (a *memoryAudit) Record(e Event) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.fail {
		return errors.New("disk full")
	}
	a.events = append(a.events, e)
	return nil
}
func testServer(a *testApprover, g *testMinter, audit Recorder) *Server {
	s := NewServer(a, g, audit)
	s.Identity = func(context.Context, string) (Caller, error) {
		return Caller{NodeID: "trusted-transport-node", Name: "cruncher"}, nil
	}
	return s
}

const validBody = `{"repositories":["zupo/test-a"],"permissions":{"contents":"read"}}`

func TestApprovalAndAudit(t *testing.T) {
	a, err := OpenAudit(filepath.Join(t.TempDir(), "audit.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	session := &testSession{c: Credentials{AccessToken: "parent-secret"}}
	g := &testMinter{refresh: true}
	s := testServer(&testApprover{session: session}, g, a)
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody))
	r.Header.Set("X-Passlet-Node", "attacker")
	s.ServeHTTP(w, r)
	if w.Code != 200 || !session.closed || session.stores != 1 {
		t.Fatalf("code=%d closed=%v stores=%d", w.Code, session.closed, session.stores)
	}
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("missing no-store")
	}
	rows, err := a.db.Query("SELECT data FROM events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var data string
		rows.Scan(&data)
		if strings.Contains(data, "secret") || strings.Contains(data, "attacker") {
			t.Fatal("secret or spoofed identity in audit")
		}
		var e Event
		json.Unmarshal([]byte(data), &e)
		events = append(events, e)
	}
	if len(events) != 4 || events[3].Outcome != "delivery_written_unconfirmed" || events[3].Caller.NodeID != "trusted-transport-node" {
		t.Fatalf("unexpected audit: %+v", events)
	}
}
func TestDenialTimeoutAndDisconnect(t *testing.T) {
	for _, mode := range []string{"deny", "timeout", "disconnect"} {
		t.Run(mode, func(t *testing.T) {
			a := &testApprover{err: errors.New("approval_denied")}
			if mode != "deny" {
				a.err = nil
				a.wait = true
			}
			g := &testMinter{}
			audit := &memoryAudit{}
			s := testServer(a, g, audit)
			s.Deadline = 15 * time.Millisecond
			r := httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody))
			ctx, cancel := context.WithCancel(r.Context())
			defer cancel()
			r = r.WithContext(ctx)
			if mode == "disconnect" {
				cancel()
			}
			w := httptest.NewRecorder()
			s.ServeHTTP(w, r)
			expected := map[string]int{"deny": 403, "timeout": 504, "disconnect": 499}[mode]
			if w.Code != expected || g.calls != 0 {
				t.Fatalf("code=%d mints=%d", w.Code, g.calls)
			}
		})
	}
}
func TestBusyDoesNotQueue(t *testing.T) {
	a := &testApprover{wait: true, entered: make(chan struct{}, 1)}
	g := &testMinter{}
	s := testServer(a, g, &memoryAudit{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		s.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)).WithContext(ctx))
		close(done)
	}()
	<-a.entered
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	cancel()
	<-done
}
func TestInvalidInputNeverPrompts(t *testing.T) {
	for _, body := range []string{
		`{"repositories":[],"permissions":{"contents":"read"}}`,
		`{"repositories":["x/a","y/b"],"permissions":{"contents":"read"}}`,
		`{"repositories":["x/a"],"permissions":{"administration":"write"}}`,
		`{"repositories":["x/a"],"permissions":{"contents":"read"},"ttl_seconds":60}`,
		validBody + ` garbage`, validBody + ` {}`, strings.Repeat("x", 9000),
	} {
		a := &testApprover{entered: make(chan struct{}, 1)}
		s := testServer(a, &testMinter{}, &memoryAudit{})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatal(w.Code)
		}
		if len(a.entered) != 0 {
			t.Fatal("invalid input prompted")
		}
	}
}
func TestAuditFailurePreventsApproval(t *testing.T) {
	a := &testApprover{entered: make(chan struct{}, 1)}
	s := testServer(a, &testMinter{}, &memoryAudit{fail: true})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if w.Code != 503 || len(a.entered) != 0 {
		t.Fatal("failed open")
	}
}
func TestIdentityFailurePreventsApproval(t *testing.T) {
	s := testServer(&testApprover{}, &testMinter{}, &memoryAudit{})
	s.Identity = func(context.Context, string) (Caller, error) { return Caller{}, errors.New("whois failed") }
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}
func TestRequestValidationIncludesMetadata(t *testing.T) {
	var r Request
	if strictJSON([]byte(validBody), &r) != nil {
		t.Fatal("decode")
	}
	if r.Validate() != nil {
		t.Fatal("validate")
	}
	if r.Permissions["metadata"] != "read" || !strings.Contains(r.Prompt(Caller{Name: "node\nforged"}), "metadata: read") {
		t.Fatal("metadata missing")
	}
	if strings.Contains(r.Prompt(Caller{Name: "node\nforged"}), "node\nforged") {
		t.Fatal("caller control injection")
	}
}
func TestStrictJSONTrailingData(t *testing.T) {
	var r Request
	if strictJSON(append([]byte(validBody), []byte("garbage")...), &r) == nil {
		t.Fatal("trailing junk accepted")
	}
	if strictJSON(bytes.TrimSpace([]byte(validBody)), &r) != nil {
		t.Fatal("valid request rejected")
	}
}

var _ http.Handler = (*Server)(nil)

func TestRealDisconnectCancelsApproval(t *testing.T) {
	a := &testApprover{wait: true, entered: make(chan struct{}, 1)}
	audit := &memoryAudit{}
	s := testServer(a, &testMinter{}, audit)
	h := httptest.NewServer(s)
	defer h.Close()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "POST", h.URL+"/v1/grants", strings.NewReader(validBody))
	done := make(chan struct{})
	go func() {
		resp, _ := h.Client().Do(req)
		if resp != nil {
			resp.Body.Close()
		}
		close(done)
	}()
	<-a.entered
	cancel()
	<-done
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		audit.mu.Lock()
		cancelled := len(audit.events) > 1 && audit.events[len(audit.events)-1].Outcome == "request_cancelled"
		audit.mu.Unlock()
		if cancelled {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("TCP disconnect did not cancel pending approval")
}

func TestBodyDeadlineDoesNotCancelPendingApproval(t *testing.T) {
	a := &testApprover{wait: true, entered: make(chan struct{}, 1)}
	s := testServer(a, &testMinter{}, &memoryAudit{})
	s.Deadline = 100 * time.Millisecond
	h := httptest.NewUnstartedServer(s)
	h.Config.ReadTimeout = 10 * time.Millisecond
	h.Start()
	defer h.Close()
	start := time.Now()
	resp, err := h.Client().Post(h.URL+"/v1/grants", "application/json", strings.NewReader(validBody))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 504 || time.Since(start) < 80*time.Millisecond {
		t.Fatal("body deadline cancelled human approval early")
	}
}

type failingAudit struct{ n, calls int }

func (a *failingAudit) Record(Event) error {
	a.calls++
	if a.calls == a.n {
		return errors.New("disk failure")
	}
	return nil
}
func TestAuditFailsClosedAfterApprovalOrMint(t *testing.T) {
	for _, n := range []int{2, 3} {
		session := &testSession{}
		minter := &testMinter{}
		s := testServer(&testApprover{session: session}, minter, &failingAudit{n: n})
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
		if w.Code != 503 || strings.Contains(w.Body.String(), "secret-never-audited") || !session.closed {
			t.Fatal("audit failure delivered a token or leaked session")
		}
		if n == 2 && minter.calls != 0 {
			t.Fatal("minted after approval audit failure")
		}
	}
}
func TestRotationStorageFailurePreventsMint(t *testing.T) {
	session := &testSession{storeErr: errors.New("secret-provider-error")}
	minter := &testMinter{refresh: true}
	s := testServer(&testApprover{session: session}, minter, &memoryAudit{})
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if w.Code != 503 || minter.calls != 0 || strings.Contains(w.Body.String(), "secret") || !session.closed {
		t.Fatal("failed rotation did not fail closed")
	}
}

type brokenWriter struct{ header http.Header }

func (w *brokenWriter) Header() http.Header       { return w.header }
func (w *brokenWriter) WriteHeader(int)           {}
func (w *brokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }
func TestDeliveryFailureIsAudited(t *testing.T) {
	audit := &memoryAudit{}
	s := testServer(&testApprover{session: &testSession{}}, &testMinter{}, audit)
	s.ServeHTTP(&brokenWriter{header: make(http.Header)}, httptest.NewRequest("POST", "/v1/grants", strings.NewReader(validBody)))
	if len(audit.events) != 4 || audit.events[3].Outcome != "delivery_failed" || audit.events[3].Decision != "approved" {
		t.Fatal("delivery outcome missing")
	}
}
