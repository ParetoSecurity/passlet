package passlet

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os/exec"
	"time"
)

type proxyConn struct {
	net.Conn
	peer net.Addr
}

func (c *proxyConn) RemoteAddr() net.Addr { return c.peer }

// ProxyListener must wrap a loopback-only listener reachable only by the trusted
// local Serve process and trusted local users. PROXY headers are not signatures.
type ProxyListener struct{ net.Listener }

func (p *ProxyListener) Accept() (net.Conn, error) {
	for {
		c, err := p.Listener.Accept()
		if err != nil {
			return nil, err
		}
		c.SetReadDeadline(time.Now().Add(5 * time.Second))
		peer, err := readProxy(c)
		if err != nil {
			c.Close()
			continue
		}
		c.SetReadDeadline(time.Time{})
		return &proxyConn{Conn: c, peer: peer}, nil
	}
}
func readProxy(r io.Reader) (net.Addr, error) {
	h := make([]byte, 16)
	if _, err := io.ReadFull(r, h); err != nil {
		return nil, err
	}
	if !bytes.Equal(h[:12], []byte("\r\n\r\n\x00\r\nQUIT\n")) || h[12] != 0x21 {
		return nil, errors.New("invalid_proxy_header")
	}
	n := int(binary.BigEndian.Uint16(h[14:]))
	if n > 4096 {
		return nil, errors.New("oversized_proxy_header")
	}
	data := make([]byte, n)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}
	switch h[13] {
	case 0x11:
		if n >= 12 {
			return &net.TCPAddr{IP: net.IP(data[:4]), Port: int(binary.BigEndian.Uint16(data[8:10]))}, nil
		}
	case 0x21:
		if n >= 36 {
			return &net.TCPAddr{IP: net.IP(data[:16]), Port: int(binary.BigEndian.Uint16(data[32:34]))}, nil
		}
	}
	return nil, errors.New("unsupported_proxy_address")
}
func WhoIs(ctx context.Context, remote string) (Caller, error) {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return Caller{}, errors.New("invalid_peer")
	}
	ip := net.ParseIP(host)
	if ip == nil || ip.IsLoopback() {
		return Caller{}, errors.New("invalid_peer")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// The peer comes from Serve's PROXY header, never an HTTP header or JSON field.
	data, err := exec.CommandContext(ctx, "tailscale", "whois", "--json", host).Output()
	if err != nil || len(data) > 1<<20 {
		return Caller{}, errors.New("identity_unavailable")
	}
	var v struct {
		Node struct {
			StableID string
			Name     string
			Tags     []string
		}
		UserProfile struct{ LoginName string }
	}
	if json.Unmarshal(data, &v) != nil || v.Node.StableID == "" || v.Node.Name == "" {
		return Caller{}, errors.New("identity_unavailable")
	}
	return Caller{NodeID: v.Node.StableID, Name: v.Node.Name, Tags: v.Node.Tags, User: v.UserProfile.LoginName}, nil
}
