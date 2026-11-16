package passlet

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"path/filepath"
	"sync"
	"time"
)

const agentPacketLimit = 256 * 1024
const agentFailure = 5

func SSHAgentSocket() string { return filepath.Join(StateDir(), "ssh-agent.sock") }

func appendAgentString(data, value []byte) []byte {
	data = binary.BigEndian.AppendUint32(data, uint32(len(value)))
	return append(data, value...)
}

func readAgentPacket(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := binary.BigEndian.Uint32(header[:])
	if size == 0 || size > agentPacketLimit {
		return nil, errors.New("invalid SSH-agent packet size")
	}
	data := make([]byte, size)
	_, err := io.ReadFull(r, data)
	return data, err
}

func writeAgentPacket(w io.Writer, data []byte) error {
	packet := appendAgentString(nil, data)
	_, err := io.Copy(w, bytes.NewReader(packet))
	return err
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
