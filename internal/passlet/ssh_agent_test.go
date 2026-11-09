package passlet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/binary"
	"errors"
	"fmt"
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

func sshAgentTestHome(t *testing.T) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "pl-ssh-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("HOME", dir)
}

func agentTestSocket(t *testing.T, exchange func([]byte) []byte) string {
	t.Helper()
	socket := filepath.Join(os.Getenv("HOME"), "upstream.sock")
	listener, err := ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); listener.Close() })
	go func() {
		for {
			c, err := listener.Accept()
			if err != nil {
				return
			}
			go serveAgentConnection(ctx, c, func(_ context.Context, data []byte) ([]byte, error) { return exchange(data), nil })
		}
	}()
	return socket
}

func identityPacket(keys ...[]byte) []byte {
	data := binary.BigEndian.AppendUint32([]byte{agentIdentitiesAnswer}, uint32(len(keys)))
	for _, key := range keys {
		data = appendAgentString(data, key)
		data = appendAgentString(data, []byte("test key"))
	}
	return data
}
func signPacket(key, data []byte) []byte {
	result := appendAgentString([]byte{agentSign}, key)
	result = appendAgentString(result, data)
	return binary.BigEndian.AppendUint32(result, 0)
}
func testAgentKey() []byte {
	return appendAgentString(appendAgentString(nil, []byte("ssh-ed25519")), testkeys.Private("ssh").Public().(ed25519.PublicKey))
}
func testAgentSign(request []byte) []byte {
	data := request[1:]
	_, _ = agentString(&data)
	challenge, _ := agentString(&data)
	sig := ed25519.Sign(testkeys.Private("ssh"), challenge)
	blob := appendAgentString(appendAgentString(nil, []byte("ssh-ed25519")), sig)
	return appendAgentString([]byte{agentSignAnswer}, blob)
}
func forwardingFixture(t *testing.T) (*SSHAgentForwarder, string, []byte) {
	t.Helper()
	sshAgentTestHome(t)
	f := NewSSHAgentForwarder()
	f.agents = func() (map[string]string, error) {
		return map[string]string{"one": "alice@cruncher", "two": "bob@cruncher"}, nil
	}
	key := testAgentKey()
	socket := agentTestSocket(t, func(data []byte) []byte {
		if bytes.Equal(data, []byte{agentIdentities}) {
			return identityPacket(key)
		}
		return testAgentSign(data)
	})
	f.Socket = socket
	return f, socket, key
}

func TestSSHAgentRejectsMutationsAndMalformedRequests(t *testing.T) {
	f, _, key := forwardingFixture(t)
	requests := [][]byte{nil, {agentIdentities, 0}, {17}, {18}, {19}, {22}, {23}, {27}, {agentSign}, appendAgentString([]byte{agentSign}, key), appendAgentString([]byte{agentSign}, []byte("other key"))}
	invalidFlags := signPacket(key, []byte("data"))
	invalidFlags[len(invalidFlags)-1] = 1
	requests = append(requests, invalidFlags)
	for _, request := range requests {
		response, err := f.forward(context.Background(), "one", request)
		if err != nil || !bytes.Equal(response, []byte{agentFailure}) {
			t.Fatalf("%x: %x %v", request, response, err)
		}
	}
}

func TestSSHAgentPacketValidation(t *testing.T) {
	for _, data := range [][]byte{nil, {0, 0, 0}, {0, 0, 0, 0}, {0, 4, 0, 1}, {0, 0, 0, 4, 1}} {
		if _, err := readAgentPacket(bytes.NewReader(data)); err == nil {
			t.Fatalf("accepted %x", data)
		}
	}
	for _, data := range [][]byte{nil, {agentFailure}, identityPacket(nil), {agentIdentitiesAnswer, 0, 0, 8, 1}, {agentIdentitiesAnswer, 0, 0, 0, 1}, append(identityPacket([]byte("key")), 1), {agentIdentitiesAnswer, 0, 0, 0, 1, 0, 0, 0, 1, 1}, {agentIdentitiesAnswer, 0, 0, 0, 1, 0, 0, 0, 8, 1}} {
		if _, err := parseAgentIdentities(data); err == nil {
			t.Fatalf("accepted keys %x", data)
		}
	}
}

func TestSSHAgentRemoteHTTP(t *testing.T) {
	f, _, key := forwardingFixture(t)
	for _, tc := range []struct {
		method string
		body   []byte
		caller bool
		origin string
		status int
	}{
		{"GET", nil, false, "", 403}, {"POST", nil, true, "browser", 403}, {"POST", nil, false, "", 403}, {"POST", make([]byte, agentPacketLimit+1), true, "", 400}, {"POST", []byte{agentIdentities}, true, "", 200},
	} {
		r := httptest.NewRequest(tc.method, "/v1/ssh-agent", bytes.NewReader(tc.body))
		r.Header.Set("Origin", tc.origin)
		if tc.caller {
			r = r.WithContext(context.WithValue(r.Context(), callerKey{}, Caller{KeyID: "one"}))
		}
		w := httptest.NewRecorder()
		f.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	f.agents = func() (map[string]string, error) { return map[string]string{}, nil }
	r := httptest.NewRequest("POST", "/v1/ssh-agent", bytes.NewReader(signPacket(key, []byte("data"))))
	r = r.WithContext(context.WithValue(r.Context(), callerKey{}, Caller{KeyID: "one"}))
	w := httptest.NewRecorder()
	f.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal(w.Code)
	}
}

func TestSSHAgentProxyEndToEnd(t *testing.T) {
	f, _, key := forwardingFixture(t)
	id := keyID(testkeys.Private("agent").Public().(ed25519.PublicKey))
	f.agents = func() (map[string]string, error) { return map[string]string{id: "alice@cruncher"}, nil }
	var connections atomic.Int32
	server := &http.Server{Handler: f, ConnState: func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			connections.Add(1)
		}
	}}
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go server.Serve(pairedFixture(t, server, backend))
	defer server.Close()
	endpoint := "https://" + backend.Addr().String()
	listener, err := ListenUnix(SSHAgentSocket())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeSSHAgent(ctx, listener, endpoint) }()
	conn, err := net.Dial("unix", SSHAgentSocket())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, request := range [][]byte{{agentIdentities}, signPacket(key, []byte("challenge"))} {
		if err := writeAgentPacket(conn, request); err != nil {
			t.Fatal(err)
		}
		response, err := readAgentPacket(conn)
		if err != nil || response[0] == agentFailure {
			t.Fatal(response, err)
		}
	}
	if connections.Load() != 2 {
		t.Fatal("requests retained network connection", connections.Load())
	}
	f.agents = func() (map[string]string, error) { return map[string]string{}, nil }
	writeAgentPacket(conn, []byte{agentIdentities})
	response, err := readAgentPacket(conn)
	if err != nil || !bytes.Equal(response, []byte{agentFailure}) {
		t.Fatal("existing connection bypassed revocation", response, err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if _, err := remoteAgentPacket(context.Background(), "invalid", nil); err == nil {
		t.Fatal("invalid endpoint")
	}
	if err := ServeSSHAgent(context.Background(), listener, "invalid"); err == nil {
		t.Fatal("invalid endpoint")
	}
	if err := ServeSSHAgent(context.Background(), listener, endpoint); err == nil {
		t.Fatal("closed listener")
	}
	server.Close()
	if _, err := remoteAgentPacket(context.Background(), endpoint, nil); err == nil {
		t.Fatal("closed backend")
	}
}

func TestSSHAgentConnectionErrors(t *testing.T) {
	a, b := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		serveAgentConnection(ctx, a, func(context.Context, []byte) ([]byte, error) { cancel(); return nil, errors.New("failed") })
		close(done)
	}()
	_ = writeAgentPacket(b, []byte{agentIdentities})
	<-done
	b.Close()
	if err := writeAgentPacket(errorWriter{}, []byte{1}); err == nil {
		t.Fatal("ignored write error")
	}
}

type errorWriter struct{}

func (errorWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func TestSSHAgentUpstreamFailures(t *testing.T) {
	for _, response := range [][]byte{{agentFailure}, {}, {agentSignAnswer}, {agentSignAnswer, 0, 0, 0, 0}, {agentIdentitiesAnswer}} {
		t.Run(fmt.Sprintf("%x", response), func(t *testing.T) {
			sshAgentTestHome(t)
			f := NewSSHAgentForwarder()
			f.agents = func() (map[string]string, error) { return map[string]string{"one": "alice"}, nil }
			key := testAgentKey()
			socket := agentTestSocket(t, func(data []byte) []byte {
				if data[0] == agentIdentities {
					return identityPacket(key)
				}
				return response
			})
			f.Socket = socket
			result, err := f.forward(context.Background(), "one", signPacket(key, []byte("test")))
			if bytes.Equal(response, []byte{agentFailure}) {
				if err != nil || !bytes.Equal(result, response) {
					t.Fatal(result, err)
				}
			} else if err == nil {
				t.Fatal("bad signature accepted")
			}
			f.Socket = "/missing/upstream.sock"
			if _, err := f.forward(context.Background(), "one", signPacket(key, []byte("test"))); err == nil {
				t.Fatal("missing upstream accepted")
			}
		})
	}
	t.Run("invalid key listing", func(t *testing.T) {
		sshAgentTestHome(t)
		f := NewSSHAgentForwarder()
		f.agents = func() (map[string]string, error) { return map[string]string{"one": "alice"}, nil }
		socket := agentTestSocket(t, func([]byte) []byte { return []byte{agentFailure} })
		f.Socket = socket
		if _, err := f.forward(context.Background(), "one", []byte{agentIdentities}); err == nil {
			t.Fatal("bad keys accepted")
		}
	})
}

func TestSSHAgentUpstreamCancellation(t *testing.T) {
	sshAgentTestHome(t)
	socket := filepath.Join(os.Getenv("HOME"), "cancel.sock")
	listener, err := ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	for _, cancelRead := range []bool{true, false} {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			if cancelRead {
				_, _ = readAgentPacket(conn)
				cancel()
				_, _ = io.Copy(io.Discard, conn)
			}
		}()
		request := []byte{agentIdentities}
		if !cancelRead {
			request = make([]byte, 4*agentPacketLimit)
		}
		if _, err := agentRoundTrip(ctx, socket, request); err == nil {
			t.Fatal("socket failure ignored")
		}
		cancel()
		<-done
	}
}

func TestSSHAgentHTTPResponseFailures(t *testing.T) {
	sshAgentTestHome(t)
	backend, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, strings.Repeat("x", agentPacketLimit+1)) })}
	go remote.Serve(pairedFixture(t, remote, backend))
	defer remote.Close()
	if _, err := remoteAgentPacket(context.Background(), "https://"+backend.Addr().String(), []byte{agentIdentities}); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestSSHAgentConnectionLimit(t *testing.T) {
	sshAgentTestHome(t)
	listener, err := ListenUnix(SSHAgentSocket())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- ServeSSHAgent(ctx, listener, "https://127.0.0.1:1") }()
	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	for range 33 {
		conn, err := net.Dial("unix", SSHAgentSocket())
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, conn)
	}
	last := clients[len(clients)-1]
	last.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, err := last.Read(b[:]); err != io.EOF {
		t.Fatal("extra connection was not rejected", err)
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestSSHAgentRejectsImpersonation(t *testing.T) {
	f, _, _ := forwardingFixture(t)
	id := keyID(testkeys.Private("agent").Public().(ed25519.PublicKey))
	f.agents = func() (map[string]string, error) { return map[string]string{id: "alice@cruncher"}, nil }
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: f}
	go server.Serve(pairedFixture(t, server, listener))
	defer server.Close()
	endpoint := "https://" + listener.Addr().String()
	testkeys.WriteNamed(t, os.Getenv("HOME"), "impostor")
	if _, err := remoteAgentPacket(context.Background(), endpoint, []byte{agentIdentities}); err == nil {
		t.Fatal("wrong approver pin accepted")
	}
	if err := testkeys.SaveEndpoint(endpoint); err != nil {
		t.Fatal(err)
	}
	if _, err := remoteAgentPacket(context.Background(), endpoint, []byte{agentIdentities}); err == nil {
		t.Fatal("unpaired client accepted")
	}
}

func TestSSHAgentConfiguredForwarding(t *testing.T) {
	f, socket, key := forwardingFixture(t)
	ctx := context.Background()
	for _, path := range []string{"", "relative", socket + "/../agent", SSHAgentSocket(), DefaultSocket(), "/missing/agent.sock"} {
		f.Socket = path
		if _, err := f.forward(ctx, "one", []byte{agentIdentities}); err == nil {
			t.Fatalf("accepted socket %q", path)
		}
	}
	f.Socket = socket
	if _, err := f.forward(ctx, "unknown", []byte{agentIdentities}); err == nil {
		t.Fatal("unpaired agent accepted")
	}
	challenge := []byte("fresh sudo challenge")
	response, err := f.forward(ctx, "one", signPacket(key, challenge))
	if err != nil {
		t.Fatal(err)
	}
	data := response[1:]
	blob, _ := agentString(&data)
	_, _ = agentString(&blob)
	signature, _ := agentString(&blob)
	public := testkeys.Private("ssh").Public().(ed25519.PublicKey)
	if !ed25519.Verify(public, challenge, signature) || ed25519.Verify(public, []byte("another challenge"), signature) {
		t.Fatal("signature or replay verification failed")
	}
	f.agents = func() (map[string]string, error) { return nil, errors.New("broken pairings") }
	if _, err := f.forward(ctx, "one", []byte{agentIdentities}); err == nil {
		t.Fatal("ignored pairing error")
	}
}

func TestSSHAgentPairingRevokedDuringSigning(t *testing.T) {
	sshAgentTestHome(t)
	f := NewSSHAgentForwarder()
	var revoked atomic.Bool
	f.agents = func() (map[string]string, error) {
		if revoked.Load() {
			return map[string]string{}, nil
		}
		return map[string]string{"one": "alice"}, nil
	}
	f.Socket = agentTestSocket(t, func(data []byte) []byte {
		revoked.Store(true)
		return testAgentSign(data)
	})
	if _, err := f.forward(context.Background(), "one", signPacket(testAgentKey(), []byte("challenge"))); err == nil {
		t.Fatal("signature released after pairing revoked")
	}
}
