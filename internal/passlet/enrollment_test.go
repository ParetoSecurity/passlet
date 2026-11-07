package passlet

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"passlet.dev/passlet/testutil/testkeys"
)

func enrollmentServer(t *testing.T, confirm func(context.Context, string) error) string {
	t.Helper()
	private, err := LoadOrCreatePairingKey(filepath.Join(StateDir(), "approver.pem"), filepath.Join(StateDir(), "approver.pub"))
	if err != nil {
		t.Fatal(err)
	}
	pairing, err := NewManagedPairing(private, confirm)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: pairing.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ready"}`)) }))}
	go server.Serve(tls.NewListener(listener, pairing.ServerConfig()))
	t.Cleanup(func() { server.Close() })
	return "https://" + listener.Addr().String()
}

func TestEnrollmentAndRevocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	var prompts atomic.Int32
	endpoint := enrollmentServer(t, func(_ context.Context, prompt string) error {
		if prompt != "pair Agent alice@vm.\n\nOnly approve if you started this pairing" {
			t.Errorf("prompt %q", prompt)
		}
		prompts.Add(1)
		return nil
	})
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if err := PairAgent(context.Background(), endpoint, code, "alice@vm"); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 1 {
		t.Fatal("missing confirmation")
	}
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	saved, err := ResolveEndpoint("")
	if err != nil || saved != endpoint {
		t.Fatal(saved, err)
	}
	if err := PairAgent(context.Background(), endpoint, code, "alice@vm"); err == nil {
		t.Fatal("replayed code accepted")
	}
	if prompts.Load() != 1 {
		t.Fatal("replay prompted")
	}
	agents, err := PairingAgents()
	if err != nil || len(agents) != 1 {
		t.Fatal(agents, err)
	}
	if err := RevokeAgent("alice@vm"); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), endpoint); err == nil {
		t.Fatal("revoked Agent retained access")
	}
	if err := RevokeAgent("alice@vm"); err == nil {
		t.Fatal("missing revocation succeeded")
	}
}

func TestEnrollmentRejections(t *testing.T) {
	for _, mode := range []string{"denied", "expired", "wrong secret", "wrong identity", "invalid name", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			var prompts atomic.Int32
			endpoint := enrollmentServer(t, func(ctx context.Context, _ string) error { prompts.Add(1); return errors.New("denied") })
			code, err := CreateInvitation()
			if err != nil {
				t.Fatal(err)
			}
			name := "alice@vm"
			switch mode {
			case "expired":
				withPairingState(true, func(s *pairingState) error { s.Expires = time.Now().Add(-time.Second); return nil })
			case "wrong secret":
				parts := strings.Split(code, ".")
				parts[1] = strings.Repeat("A", 43)
				code = strings.Join(parts, ".")
			case "wrong identity":
				parts := strings.Split(code, ".")
				parts[0] = strings.Repeat("A", 43)
				code = strings.Join(parts, ".")
			case "invalid name":
				name = "alice\ntrusted"
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if err := PairAgent(ctx, endpoint, code, name); err == nil {
				t.Fatal("invalid enrollment accepted")
			}
			if _, err := os.Stat(connectionPath()); !os.IsNotExist(err) {
				t.Fatal("failed pairing saved connection")
			}
			agents, err := PairingAgents()
			if err != nil || len(agents) != 0 {
				t.Fatal(agents, err)
			}
			want := int32(0)
			if mode == "denied" {
				want = 1
			}
			if prompts.Load() != want {
				t.Fatal("unexpected confirmation", prompts.Load())
			}
		})
	}
}

func TestInvitationReplacementAndInvalidInput(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	endpoint := enrollmentServer(t, func(context.Context, string) error { return nil })
	old, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	current, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if old == current {
		t.Fatal("code reused")
	}
	if err := PairAgent(context.Background(), endpoint, old, "alice@vm"); err == nil {
		t.Fatal("superseded code accepted")
	}
	for _, code := range []string{"", "a.b", "a.b.c", strings.Repeat("A", 43) + ".invalid"} {
		if err := PairAgent(context.Background(), endpoint, code, "alice@vm"); err == nil {
			t.Fatal("invalid code accepted")
		}
	}
	if err := PairAgent(context.Background(), "http://unsafe", current, "alice@vm"); err == nil {
		t.Fatal("cleartext endpoint accepted")
	}
	testkeys.Write(t, os.Getenv("HOME"))
}

func TestPairingStorageFailures(t *testing.T) {
	for _, mode := range []string{"shared directory", "lock directory", "state directory", "invalid state", "invalid key"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			os.MkdirAll(StateDir(), 0700)
			switch mode {
			case "shared directory":
				os.Chmod(StateDir(), 0755)
			case "lock directory":
				os.Mkdir(filepath.Join(StateDir(), "pairing.lock"), 0700)
			case "state directory":
				os.Mkdir(filepath.Join(StateDir(), "pairings.json"), 0700)
			case "invalid state":
				os.WriteFile(filepath.Join(StateDir(), "pairings.json"), []byte(`{"agents":null}`), 0600)
			case "invalid key":
				os.WriteFile(filepath.Join(StateDir(), "approver.pem"), []byte("invalid"), 0600)
			}
			if _, err := CreateInvitation(); err == nil {
				t.Fatal("invalid storage accepted")
			}
		})
	}
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(StateDir(), 0700)
	for _, data := range []string{`{"endpoint":"bad","approver":"bad"}`, `{"endpoint":"https://host","approver":"bad"}`} {
		os.WriteFile(connectionPath(), []byte(data), 0600)
		if _, err := readConnection(); err == nil {
			t.Fatal("invalid connection accepted")
		}
	}
	if _, err := NewManagedPairing(nil, func(context.Context, string) error { return nil }); err == nil {
		t.Fatal("invalid server key accepted")
	}
}

func TestEnrollmentProtocolBoundaries(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	private, err := ReadPrivateKey(filepath.Join(StateDir(), "approver.pem"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := NewManagedPairing(private, func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	cert, err := certificateFor(testkeys.Private("agent"))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(cert.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	secret := strings.Split(code, ".")[1]
	for _, mode := range []string{"method", "origin", "certificate", "body", "secret", "busy", "cancelled", "duplicate"} {
		t.Run(mode, func(t *testing.T) {
			code, err := CreateInvitation()
			if err != nil {
				t.Fatal(err)
			}
			secret = strings.Split(code, ".")[1]
			body := `{"name":"alice@vm","secret":"` + secret + `"}`
			if mode == "body" {
				body = strings.Repeat("x", 2049)
			}
			if mode == "secret" {
				body = `{"name":"alice@vm","secret":"bad"}`
			}
			r := httptest.NewRequest("POST", "https://passlet/v1/pair", strings.NewReader(body))
			r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsed}}
			if mode == "method" {
				r.Method = "GET"
			}
			if mode == "origin" {
				r.Header.Set("Origin", "https://example.test")
			}
			if mode == "certificate" {
				r.TLS.PeerCertificates = nil
			}
			if mode == "busy" {
				p.enrollmentBusy <- struct{}{}
				defer func() { <-p.enrollmentBusy }()
			}
			if mode == "cancelled" {
				ctx, cancel := context.WithCancel(r.Context())
				cancel()
				r = r.WithContext(ctx)
			}
			if mode == "duplicate" {
				withPairingState(true, func(s *pairingState) error { s.Agents["other"] = pairedAgent{Name: "alice@vm"}; return nil })
			}
			out := httptest.NewRecorder()
			p.enroll(out, r)
			if out.Code == 200 {
				t.Fatal("accepted", mode)
			}
		})
	}
	os.WriteFile(filepath.Join(StateDir(), "pairings.json"), []byte("broken"), 0600)
	r := httptest.NewRequest("GET", "https://passlet/v1/health", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{parsed}}
	out := httptest.NewRecorder()
	p.Handler(http.NotFoundHandler()).ServeHTTP(out, r)
	if out.Code != 503 {
		t.Fatal(out.Code)
	}
}

func TestEnrollmentClientFailures(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	if err := PairAgent(context.Background(), "unix:///tmp/passlet-nonexistent-enrollment.sock", code, "alice@vm"); err == nil {
		t.Fatal("unreachable socket accepted")
	}
	os.WriteFile(filepath.Join(StateDir(), "agent.pem"), []byte("invalid"), 0600)
	if err := PairAgent(context.Background(), "https://127.0.0.1:1", code, "alice@vm"); err == nil {
		t.Fatal("invalid private key accepted")
	}
}

func TestEnrollmentUntrustedResponses(t *testing.T) {
	for _, mode := range []string{"invalid json", "truncated", "redirect", "nil context"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			code, err := CreateInvitation()
			if err != nil {
				t.Fatal(err)
			}
			private, err := ReadPrivateKey(filepath.Join(StateDir(), "approver.pem"))
			if err != nil {
				t.Fatal(err)
			}
			p, err := NewManagedPairing(private, func(context.Context, string) error { return nil })
			if err != nil {
				t.Fatal(err)
			}
			l, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "redirect" {
					http.Redirect(w, r, "https://elsewhere.invalid", http.StatusFound)
					return
				}
				if mode == "truncated" {
					w.Header().Set("Content-Length", "1000")
				}
				w.Write([]byte("invalid"))
			})}
			go server.Serve(tls.NewListener(l, p.ServerConfig()))
			defer server.Close()
			ctx := context.Background()
			if mode == "nil context" {
				ctx = nil
			}
			if err := PairAgent(ctx, "https://"+l.Addr().String(), code, "alice@vm"); err == nil {
				t.Fatal("invalid response accepted")
			}
		})
	}
}

func TestPairedTLSRejectsUnreadablePrivateKey(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testkeys.Write(t, os.Getenv("HOME"))
	os.WriteFile(filepath.Join(StateDir(), "agent.pem"), []byte("invalid"), 0600)
	if _, err := configuredAgentTLS(); err == nil {
		t.Fatal("invalid key accepted")
	}
}

func TestConcurrentEnrollmentAndOpenConnectionRevocation(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var prompts atomic.Int32
	endpoint := enrollmentServer(t, func(context.Context, string) error { prompts.Add(1); return nil })
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() { results <- PairAgent(context.Background(), endpoint, code, "alice@vm") }()
	}
	successes := 0
	for i := 0; i < 2; i++ {
		if <-results == nil {
			successes++
		}
	}
	if successes != 1 || prompts.Load() != 1 {
		t.Fatal(successes, prompts.Load())
	}
	client, closeClient, err := EndpointClient(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer closeClient()
	address, _ := EndpointURL(endpoint, "/v1/health")
	response, err := client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if err := RevokeAgent("alice@vm"); err != nil {
		t.Fatal(err)
	}
	response, err = client.Get(address)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != 403 {
		t.Fatal("revoked existing connection accepted", response.StatusCode)
	}
}

func TestSharedPairingStateDirectory(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	os.MkdirAll(StateDir(), 0755)
	os.Chmod(StateDir(), 0755)
	if _, err := PairingAgents(); err == nil {
		t.Fatal("shared state directory accepted")
	}
}

func TestPairThroughAuthenticatedProxy(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("PASSLET_ENDPOINT", "")
	var prompts atomic.Int32
	endpoint := enrollmentServer(t, func(context.Context, string) error {
		prompts.Add(1)
		return nil
	})
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	proxy, tunnels := connectProxy(t, true)
	useProxy(t, proxy)
	if err := PairAgent(context.Background(), endpoint, code, "alice@proxy"); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 1 || tunnels.Load() != 2 {
		t.Fatalf("prompts=%d tunnels=%d", prompts.Load(), tunnels.Load())
	}
	proxy.User = nil
	if err := Health(context.Background(), endpoint); err == nil {
		t.Fatal("proxy accepted missing authentication")
	}
}

func TestPairExplainsDeniedSandboxSocket(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CODEX_SANDBOX_NETWORK_DISABLED", "1")
	code, err := CreateInvitation()
	if err != nil {
		t.Fatal(err)
	}
	blocked, err := os.MkdirTemp("/tmp", "pl-denied-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(blocked)
	if err := os.Chmod(blocked, 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0700)
	err = PairAgent(context.Background(), "unix://"+filepath.Join(blocked, "socket"), code, "alice@vm")
	if err == nil || !strings.Contains(err.Error(), "Codex sandbox denied") {
		t.Fatal(err)
	}
}
