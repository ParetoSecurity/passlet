package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os/exec"
	"time"
)

type tailscaleConn struct{ net.Conn }

// The dedicated socket is private because Serve's forwarded headers are trusted.
type TailscaleListener struct{ net.Listener }

func (l *TailscaleListener) Accept() (net.Conn, error) {
	c, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &tailscaleConn{c}, nil
}

func tailscalePeer(r *http.Request) (string, error) {
	forwarded := r.Header.Values("X-Forwarded-For")
	if len(forwarded) != 1 || r.Header.Get("Tailscale-Funnel-Request") != "" {
		return "", errors.New("invalid_tailscale_peer")
	}
	ip := net.ParseIP(forwarded[0])
	if ip == nil || ip.IsLoopback() {
		return "", errors.New("invalid_tailscale_peer")
	}
	return net.JoinHostPort(ip.String(), "0"), nil
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
	// Only the dedicated Serve socket accepts a forwarded peer address.
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
