package passlet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

func (f *SSHAgentForwarder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != "POST" || r.Header.Get("Origin") != "" {
		apiError(w, 403, "invalid SSH-agent request")
		return
	}
	caller, err := connectionIdentity(r.Context(), r.RemoteAddr)
	if err != nil {
		apiError(w, 403, "agent_authentication_required")
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, agentPacketLimit))
	if err != nil {
		apiError(w, 400, "SSH-agent request too large")
		return
	}
	_ = http.NewResponseController(w).SetReadDeadline(time.Time{})
	response, err := f.forward(r.Context(), caller.KeyID, data)
	if err != nil {
		apiError(w, 403, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(response)
}

func remoteAgentPacket(ctx context.Context, endpoint string, data []byte) ([]byte, error) {
	client, closeClient, err := EndpointClient(endpoint)
	if err != nil {
		return nil, err
	}
	defer closeClient()
	client.Transport.(*http.Transport).DisableKeepAlives = true
	target, _ := EndpointURL(endpoint, "/v1/ssh-agent")
	req, _ := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/octet-stream")
	response, err := client.Do(req)
	if err != nil {
		return nil, authenticationFailure(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, agentPacketLimit+1))
	if err != nil || len(body) == 0 || len(body) > agentPacketLimit {
		return nil, errors.New("invalid SSH-agent proxy response")
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("SSH-agent forwarding unavailable; check the SSH-agent configuration on your Mac")
	}
	return body, nil
}

// ServeSSHAgent keeps only a local socket open. Each protocol message creates a
// fresh authenticated request, so an idle Rex session holds no network tunnel.
func ServeSSHAgent(ctx context.Context, listener net.Listener, endpoint string) error {
	if _, err := endpointURL(endpoint); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	defer func() { cancel(); workers.Wait() }()
	stop := context.AfterFunc(ctx, func() { _ = listener.Close() })
	defer stop()
	slots := make(chan struct{}, 32)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer func() { <-slots }()
				ServeSSHAgentConnection(ctx, conn, endpoint)
			}()
		default:
			_ = conn.Close()
		}
	}
}

func ServeSSHAgentConnection(ctx context.Context, conn net.Conn, endpoint string) {
	serveAgentConnection(ctx, conn, func(ctx context.Context, data []byte) ([]byte, error) { return remoteAgentPacket(ctx, endpoint, data) })
}

func serveAgentConnection(ctx context.Context, conn net.Conn, exchange func(context.Context, []byte) ([]byte, error)) {
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	for {
		_ = conn.SetDeadline(time.Now().Add(ClientDeadline))
		data, err := readAgentPacket(conn)
		if err != nil {
			return
		}
		response, err := exchange(ctx, data)
		if err != nil {
			response = []byte{agentFailure}
		}
		if err := writeAgentPacket(conn, response); err != nil {
			return
		}
	}
}
