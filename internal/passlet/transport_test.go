package passlet

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWhoIsUsesTransportAddress(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$1" = whois ] && [ "$2" = --json ] && [ "$3" = 100.72.70.9 ] || exit 2
printf '%s\n' '{"Node":{"StableID":"real-node","Name":"cruncher.tailnet.ts.net.","Tags":["tag:cruncher"]},"UserProfile":{"LoginName":"tagged-devices"}}'
`
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	c, err := WhoIs(context.Background(), "100.72.70.9:45678")
	if err != nil || c.NodeID != "real-node" || c.User != "tagged-devices" || len(c.Tags) != 1 {
		t.Fatal("WhoIs lost transport identity", err)
	}
	for _, peer := range []string{"127.0.0.1:1", "evil.example:1", "invalid", "100.72.70.8:1"} {
		if _, err := WhoIs(context.Background(), peer); err == nil {
			t.Fatal("unexpected identity accepted")
		}
	}
	os.WriteFile(filepath.Join(dir, "tailscale"), []byte("#!/bin/sh\nprintf '%s' '{\"Node\":{}}'\n"), 0700)
	if _, err := WhoIs(context.Background(), "100.72.70.9:1"); err == nil {
		t.Fatal("empty WhoIs identity accepted")
	}
}

func TestTailscaleSocketPreservesIdentityAndRejectsSpoofing(t *testing.T) {
	dir := t.TempDir()
	script := `#!/bin/sh
[ "$1" = whois ] && [ "$2" = --json ] && [ "$3" = 100.72.70.9 ] || exit 2
printf '%s\n' '{"Node":{"StableID":"real-node","Name":"cruncher.tailnet.ts.net."}}'
`
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	for _, proxied := range []bool{false, true} {
		path := socketPath(t)
		listener, err := ListenUnix(path)
		if err != nil {
			t.Fatal(err)
		}
		serving := listener
		if proxied {
			serving = &TailscaleListener{listener}
		}
		handler := NewServer(nil, nil, nil)
		identity := handler.Identity
		handler.Identity = func(ctx context.Context, remote string) (Caller, error) {
			caller, err := identity(ctx, remote)
			if err == nil && proxied && (caller.Transport != "tailscale" || caller.NodeID != "real-node" || caller.Name != "cruncher.tailnet.ts.net.") {
				t.Errorf("lost caller identity: %+v", caller)
			}
			if err == nil && !proxied && caller.Transport != "unix" {
				t.Error("normal socket trusted forwarded identity")
			}
			return caller, err
		}
		server := &http.Server{Handler: handler, ConnContext: ConnectionContext}
		go func() { _ = server.Serve(serving) }()
		client, closeClient, err := EndpointClient("unix://" + path)
		if err != nil {
			t.Fatal(err)
		}
		for _, tc := range []struct {
			values []string
			funnel string
			valid  bool
		}{
			{[]string{"100.72.70.9"}, "", true},
			{nil, "", false},
			{[]string{"127.0.0.1"}, "", false},
			{[]string{"100.72.70.9, 100.72.70.8"}, "", false},
			{[]string{"100.72.70.9", "100.72.70.8"}, "", false},
			{[]string{"100.72.70.8"}, "", false},
			{[]string{"100.72.70.9"}, "?1", false},
		} {
			req, err := http.NewRequest("GET", "http://passlet.local/v1/health", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range tc.values {
				req.Header.Add("X-Forwarded-For", v)
			}
			req.Header.Set("Tailscale-Funnel-Request", tc.funnel)
			req.Header.Set("Tailscale-User-Login", "forged")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			want := http.StatusOK
			if proxied && !tc.valid {
				want = http.StatusForbidden
			}
			if resp.StatusCode != want {
				t.Fatalf("proxied=%v headers=%v: %d want %d", proxied, tc.values, resp.StatusCode, want)
			}
		}
		closeClient()
		_ = server.Close()
		if proxied {
			if _, err := serving.Accept(); err == nil {
				t.Fatal("closed socket accepted")
			}
		}
	}
}

func TestSshSocketsAttributeRequestsByRoute(t *testing.T) {
	approval := &testApprover{session: &testSession{}}
	provider := &testMinter{}
	audit := &memoryAudit{}
	handler := NewServer(approval, provider, audit)
	server := &http.Server{Handler: handler, ConnContext: ConnectionContext}
	defer server.Close()
	var listeners []*SshListener
	for _, label := range []string{"cruncher", "other-server"} {
		path := socketPath(t)
		listener, err := ListenUnix(path)
		if err != nil {
			t.Fatal(err)
		}
		ssh := &SshListener{Listener: listener, Label: label}
		listeners = append(listeners, ssh)
		go func() { _ = server.Serve(ssh) }()
		client, closeClient, err := EndpointClient("unix://" + path)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", "http://passlet.local/v1/grants", strings.NewReader(validBody))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-Forwarded-For", "100.72.70.9")
		req.Header.Set("Tailscale-User-Login", "forged")
		req.Header.Set("X-Passlet-Node", "forged")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		closeClient()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("grant failed: %d", resp.StatusCode)
		}
	}
	if err := server.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(approval.prompts) != 2 || provider.calls != 2 {
		t.Fatal("each route must require approval before issuing a grant")
	}
	for i, label := range []string{"cruncher", "other-server"} {
		if !strings.HasPrefix(approval.prompts[i], "grant access to GitHub for:\n• Shell from "+label+"\n") {
			t.Fatalf("missing route label: %s", approval.prompts[i])
		}
		if _, err := listeners[i].Accept(); err == nil {
			t.Fatal("closed SSH listener accepted a connection")
		}
	}
	seen := map[string]bool{}
	for _, event := range audit.events {
		if event.Caller.Transport != "ssh" || event.Caller.NodeID != "" || event.Caller.User != "" || (event.Caller.Name != "cruncher" && event.Caller.Name != "other-server") {
			t.Fatalf("incorrect route identity: %+v", event.Caller)
		}
		seen[event.Caller.Name] = true
	}
	if len(seen) != 2 {
		t.Fatal("both routes must appear in audit")
	}
}
