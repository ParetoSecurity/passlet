package passlet

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"passlet.dev/passlet/testutil/testkeys"
)

func TestMain(m *testing.M) {
	if os.Getenv("PASSLET_VM_ROLE") != "" || os.Getenv("PASSLET_LIVE_TEST") != "" {
		os.Exit(m.Run())
	}
	home, err := os.MkdirTemp("", "passlet-tests-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}

func pairedFixture(t *testing.T, server *http.Server, listener net.Listener) net.Listener {
	t.Helper()
	testkeys.Write(t, os.Getenv("HOME"))
	p, err := NewPairing(testkeys.Private("approver"), map[string]string{"test-agent": testkeys.Public("agent")})
	if err != nil {
		t.Fatal(err)
	}
	server.Handler = p.Handler(server.Handler)
	return tls.NewListener(listener, p.ServerConfig())
}

func connectProxy(t *testing.T, authenticated bool) (*url.URL, *atomic.Int32) {
	t.Helper()
	var tunnels atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			t.Errorf("proxy received %s", r.Method)
			return
		}
		if authenticated && r.Header.Get("Proxy-Authorization") != "Basic "+base64.StdEncoding.EncodeToString([]byte("fixture:proxy-secret")) {
			w.WriteHeader(http.StatusProxyAuthRequired)
			return
		}
		tunnels.Add(1)
		target, err := net.Dial("tcp", r.Host)
		if err != nil {
			t.Error(err)
			return
		}
		defer target.Close()
		client, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer client.Close()
		_, _ = client.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		go func() { _, _ = io.Copy(target, client) }()
		_, _ = io.Copy(client, target)
	}))
	t.Cleanup(proxy.Close)
	address, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	if authenticated {
		address.User = url.UserPassword("fixture", "proxy-secret")
	}
	return address, &tunnels
}

func useProxy(t *testing.T, proxy *url.URL) {
	t.Helper()
	original := environmentProxy
	environmentProxy = func(*http.Request) (*url.URL, error) { return proxy, nil }
	t.Cleanup(func() { environmentProxy = original })
}

func TestHttpsEndpointConnectsThroughProxy(t *testing.T) {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ready"}`))
	})}
	secured := pairedFixture(t, server, listener)
	go func() { _ = server.Serve(secured) }()
	t.Cleanup(func() { _ = server.Close() })
	proxy, tunnels := connectProxy(t, false)
	useProxy(t, proxy)
	if err := Health(context.Background(), "https://"+listener.Addr().String()); err != nil {
		t.Fatal(err)
	}
	if tunnels.Load() != 1 {
		t.Fatalf("tunnels: %d", tunnels.Load())
	}
	saved, err := readConnection()
	if err != nil {
		t.Fatal(err)
	}
	saved.Approver = base64.RawURLEncoding.EncodeToString(testkeys.Private("wrong-approver").Public().(ed25519.PublicKey))
	data, _ := json.Marshal(saved)
	if err := os.WriteFile(connectionPath(), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := Health(context.Background(), "https://"+listener.Addr().String()); err == nil {
		t.Fatal("proxy bypassed Approver pin")
	}
}

func TestUnixEndpointIgnoresProxy(t *testing.T) {
	proxy, tunnels := connectProxy(t, false)
	useProxy(t, proxy)
	endpoint := unixServer(t, NewServer(&testApprover{}, &testMinter{}, &memoryAudit{}))
	if err := Health(context.Background(), endpoint); err != nil {
		t.Fatal(err)
	}
	if tunnels.Load() != 0 {
		t.Fatalf("tunnels: %d", tunnels.Load())
	}
}

func TestPairedConnections(t *testing.T) {
	for _, network := range []string{"unix", "tcp4"} {
		t.Run(network, func(t *testing.T) {
			p, err := NewPairing(testkeys.Private("approver"), map[string]string{"zupo@cruncher": testkeys.Public("agent")})
			if err != nil {
				t.Fatal(err)
			}
			address := "127.0.0.1:0"
			if network == "unix" {
				address = socketPath(t)
			}
			listener, err := net.Listen(network, address)
			if err != nil {
				t.Fatal(err)
			}
			var calls atomic.Int32
			server := &http.Server{Handler: p.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				caller, err := connectionIdentity(r.Context(), r.RemoteAddr)
				if err != nil || caller.Name != "zupo@cruncher" || caller.KeyID != keyID(testkeys.Private("agent").Public().(ed25519.PublicKey)) {
					t.Errorf("wrong paired identity: %+v %v", caller, err)
				}
				calls.Add(1)
				w.WriteHeader(http.StatusNoContent)
			})), ReadHeaderTimeout: time.Second}
			go func() { _ = server.Serve(tls.NewListener(listener, p.ServerConfig())) }()
			defer server.Close()
			for _, tc := range []struct {
				agent, approver string
				allowed         bool
			}{
				{"agent", "approver", true},
				{"unknown", "approver", false},
				{"agent", "wrong-approver", false},
			} {
				config, err := pairedTLS(testkeys.Private(tc.agent), testkeys.Private(tc.approver).Public().(ed25519.PublicKey))
				if err != nil {
					t.Fatal(err)
				}
				raw, err := net.Dial(network, listener.Addr().String())
				if err != nil {
					t.Fatal(err)
				}
				conn := tls.Client(raw, config)
				_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
				err = conn.Handshake()
				if err == nil {
					_, err = io.WriteString(conn, "POST /v1/grants HTTP/1.1\r\nHost: passlet\r\nX-Forwarded-For: 100.1.2.3\r\nX-Passlet-Node: forged\r\nContent-Length: 0\r\n\r\n")
				}
				if err == nil {
					var response *http.Response
					response, err = http.ReadResponse(bufio.NewReader(conn), nil)
					if err == nil {
						response.Body.Close()
					}
				}
				conn.Close()
				if (err == nil) != tc.allowed {
					t.Fatalf("%+v: %v", tc, err)
				}
			}
			config := &tls.Config{MinVersion: tls.VersionTLS13, InsecureSkipVerify: true}
			raw, _ := net.Dial(network, listener.Addr().String())
			conn := tls.Client(raw, config)
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_ = conn.Handshake()
			_, _ = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: passlet\r\n\r\n")
			if _, err := http.ReadResponse(bufio.NewReader(conn), nil); err == nil {
				t.Fatal("missing client key accepted")
			}
			conn.Close()
			if calls.Load() != 1 {
				t.Fatalf("unauthenticated calls reached handler: %d", calls.Load())
			}
		})
	}
}

type recordingConnection struct {
	net.Conn
	written bytes.Buffer
}

func (c *recordingConnection) Write(data []byte) (int, error) {
	n, err := c.Conn.Write(data)
	_, _ = c.written.Write(data[:n])
	return n, err
}

func TestCapturedTLSRequestCannotBeReplayed(t *testing.T) {
	p, err := NewPairing(testkeys.Private("approver"), map[string]string{"agent": testkeys.Public("agent")})
	if err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	server := &http.Server{Handler: p.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(204)
	})), ReadHeaderTimeout: time.Second}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(tls.NewListener(listener, p.ServerConfig())) }()
	defer server.Close()
	raw, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	recorded := &recordingConnection{Conn: raw}
	config, err := pairedTLS(testkeys.Private("agent"), testkeys.Private("approver").Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	conn := tls.Client(recorded, config)
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err := conn.Handshake(); err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(conn, "POST /v1/grants HTTP/1.1\r\nHost: passlet\r\nContent-Length: 0\r\n\r\n")
	response, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	conn.Close()
	replayed, err := net.Dial("tcp4", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer replayed.Close()
	_ = replayed.SetDeadline(time.Now().Add(2 * time.Second))
	_, _ = replayed.Write(recorded.written.Bytes())
	_, err = io.ReadAll(replayed)
	if err != nil {
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("replayed handshake was not rejected")
		}
	}
	if calls.Load() != 1 {
		t.Fatal("replayed TLS request reached handler")
	}
}

func TestPairingRejectsInvalidTrustConfiguration(t *testing.T) {
	for _, agents := range []map[string]string{
		{"": testkeys.Public("agent")},
		{"bad\nlabel": testkeys.Public("agent")},
		{" a": testkeys.Public("agent")},
		{strings.Repeat("a", 101): testkeys.Public("agent")},
		{"agent": "invalid"},
		{"one": testkeys.Public("agent"), "two": testkeys.Public("agent")},
	} {
		if _, err := NewPairing(testkeys.Private("approver"), agents); err == nil {
			t.Fatal("invalid trust accepted")
		}
	}
	if _, err := NewPairing(nil, nil); err == nil {
		t.Fatal("missing key accepted")
	}
	if _, err := pairedTLS(nil, nil); err == nil {
		t.Fatal("missing client key accepted")
	}
	for _, value := range []string{"", testkeys.Public("agent") + "junk", "-----BEGIN PUBLIC KEY-----\nYWJj\n-----END PUBLIC KEY-----"} {
		if _, err := ParsePublicKey(value); err == nil {
			t.Fatal("invalid public key accepted")
		}
	}
}

func TestPairingHandlerRejectsMissingOrUnknownIdentity(t *testing.T) {
	p, _ := NewPairing(testkeys.Private("approver"), map[string]string{"agent": testkeys.Public("agent")})
	handler := p.Handler(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("unauthenticated request reached handler") }))
	for _, state := range []*tls.ConnectionState{nil, {}, {PeerCertificates: []*x509.Certificate{{PublicKey: testkeys.Private("unknown").Public(), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}}}} {
		r := httptest.NewRequest("GET", "https://passlet/v1/health", nil)
		r.TLS = state
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatal(w.Code)
		}
	}
	for _, cert := range []*x509.Certificate{
		{PublicKey: "not ed25519"},
		{PublicKey: testkeys.Private("agent").Public(), NotBefore: time.Now().Add(time.Hour), NotAfter: time.Now().Add(2 * time.Hour)},
		{PublicKey: testkeys.Private("agent").Public(), NotBefore: time.Now().Add(-2 * time.Hour), NotAfter: time.Now().Add(-time.Hour)},
	} {
		if _, err := peerKey(tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}); err == nil {
			t.Fatal("invalid certificate accepted")
		}
	}
	if err := p.ServerConfig().VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Fatal("missing certificate accepted")
	}
	config, _ := pairedTLS(testkeys.Private("agent"), testkeys.Private("approver").Public().(ed25519.PublicKey))
	if err := config.VerifyConnection(tls.ConnectionState{}); err == nil {
		t.Fatal("missing approver certificate accepted")
	}
}

func TestPrivateKeyAndAgentConfiguration(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := testkeys.Write(t, home)
	if _, err := ReadPrivateKey(path); err != nil {
		t.Fatal(err)
	}
	configPath := connectionPath()
	valid, _ := os.ReadFile(configPath)
	if _, err := configuredAgentTLS(); err != nil {
		t.Fatal(err)
	}
	for _, data := range []string{
		"invalid toml", strings.Repeat(" ", 8193),
		"private_key_file = 'relative'",
		fmt.Sprintf("private_key_file = %q\napprover_public_key = 'bad'", path),
	} {
		os.WriteFile(configPath, []byte(data), 0600)
		if _, err := configuredAgentTLS(); err == nil {
			t.Fatal("invalid agent config accepted")
		}
	}
	os.Remove(configPath)
	if _, err := configuredAgentTLS(); err == nil {
		t.Fatal("missing config accepted")
	}
	os.Mkdir(configPath, 0700)
	if _, err := configuredAgentTLS(); err == nil {
		t.Fatal("directory config accepted")
	}
	os.Remove(configPath)
	os.WriteFile(configPath, valid, 0600)
	for _, candidate := range []string{"relative", filepath.Join(home, "missing"), home} {
		if _, err := ReadPrivateKey(candidate); err == nil {
			t.Fatal("invalid key path accepted")
		}
	}
	original, _ := os.ReadFile(path)
	os.Chmod(path, 0644)
	if _, err := ReadPrivateKey(path); err == nil {
		t.Fatal("shared private key accepted")
	}
	os.Chmod(path, 0600)
	for _, data := range []string{"bad", strings.Repeat(" ", 4097), string(original) + "trailing", "-----BEGIN PRIVATE KEY-----\nYWJj\n-----END PRIVATE KEY-----"} {
		os.WriteFile(path, []byte(data), 0600)
		if _, err := ReadPrivateKey(path); err == nil {
			t.Fatal("invalid private key accepted")
		}
	}
	t.Setenv("HOME", "")
	if _, err := configuredAgentTLS(); err == nil {
		t.Fatal("missing HOME accepted")
	}
	err := authenticationFailure(fmt.Errorf("wrapped: %w", authenticationError("agent_not_trusted")))
	if err.Error() != "agent_not_trusted" {
		t.Fatal(err)
	}
}

func TestPairingProblemGuidance(t *testing.T) {
	for _, tc := range []struct{ code, text string }{
		{"agent_configuration_unavailable", "Agent pairing is not configured"},
		{"invalid_agent_configuration", "Saved pairing is invalid"},
		{"agent_private_key_unavailable", "provision the key"},
		{"agent_private_key_requires_owner_and_mode_0600", "owned by you with mode 0600"},
		{"invalid_agent_private_key", "PKCS#8 PEM"},
		{"invalid_agent_public_key", "public key must be an Ed25519"},
		{"approver_identity_mismatch", "does not match"},
		{"invalid_pairing_certificate", "Check the trusted keys"},
	} {
		if got := PairingProblem(fmt.Errorf("connection: %w", authenticationError(tc.code))); !strings.Contains(got, tc.text) {
			t.Fatalf("%s: %s", tc.code, got)
		}
	}
	if got := PairingProblem(fmt.Errorf("connection unavailable")); got != "" {
		t.Fatal(got)
	}
}
