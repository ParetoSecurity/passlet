package passlet

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func proxyHeader(family byte, data []byte) []byte {
	h := append([]byte("\r\n\r\n\x00\r\nQUIT\n"), 0x21, family, 0, 0)
	binary.BigEndian.PutUint16(h[14:], uint16(len(data)))
	return append(h, data...)
}
func TestProxyProtocol(t *testing.T) {
	data := []byte{100, 72, 70, 9, 100, 106, 66, 11, 0x12, 0x34, 0x20, 0xfb}
	a, err := readProxy(bytes.NewReader(proxyHeader(0x11, data)))
	if err != nil || a.String() != "100.72.70.9:4660" {
		t.Fatalf("%v %v", a, err)
	}
	v6 := make([]byte, 36)
	v6[0] = 0xfd
	v6[15] = 1
	v6[32] = 1
	a, err = readProxy(bytes.NewReader(proxyHeader(0x21, v6)))
	if err != nil || a.String() != "[fd00::1]:256" {
		t.Fatalf("%v %v", a, err)
	}
	for _, bad := range [][]byte{[]byte("GET / HTTP/1.1\r\n"), proxyHeader(0x11, data[:2]), proxyHeader(0x12, data), proxyHeader(0x11, make([]byte, 4097))} {
		if _, err := readProxy(bytes.NewReader(bad)); err == nil {
			t.Fatal("bad proxy accepted")
		}
	}
}
func FuzzProxyHeader(f *testing.F) {
	f.Add([]byte("GET / HTTP/1.1\r\n"))
	f.Add(proxyHeader(0x11, make([]byte, 12)))
	f.Fuzz(func(t *testing.T, b []byte) { readProxy(bytes.NewReader(b)) })
}

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
