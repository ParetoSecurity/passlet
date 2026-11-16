//go:build !agentonly

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
	"time"
)

const agentIdentities = 11
const agentIdentitiesAnswer = 12
const agentSign = 13
const agentSignAnswer = 14

type agentIdentity struct{ blob, comment []byte }

type SSHAgentForwarder struct {
	Socket string
	agents func() (map[string]string, error)
}

func NewSSHAgentForwarder() *SSHAgentForwarder {
	return &SSHAgentForwarder{agents: PairingAgents}
}

func agentString(data *[]byte) ([]byte, error) {
	if len(*data) < 4 {
		return nil, errors.New("invalid SSH-agent message")
	}
	size := binary.BigEndian.Uint32((*data)[:4])
	*data = (*data)[4:]
	if uint64(size) > uint64(len(*data)) {
		return nil, errors.New("invalid SSH-agent message")
	}
	value := (*data)[:size]
	*data = (*data)[size:]
	return value, nil
}

func agentRoundTrip(ctx context.Context, socket string, request []byte) ([]byte, error) {
	conn, err := (&net.Dialer{Timeout: ConnectDeadline}).DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, errors.New("SSH agent unavailable; check its socket and unlock your Mac")
	}
	defer func() { _ = conn.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	_ = conn.SetDeadline(time.Now().Add(ClientDeadline))
	if err := writeAgentPacket(conn, request); err != nil {
		return nil, err
	}
	return readAgentPacket(conn)
}

func parseAgentIdentities(data []byte) ([]agentIdentity, error) {
	if len(data) < 5 || data[0] != agentIdentitiesAnswer {
		return nil, errors.New("SSH agent did not return its keys")
	}
	count := binary.BigEndian.Uint32(data[1:5])
	data = data[5:]
	if count > 2048 {
		return nil, errors.New("too many SSH agent keys")
	}
	keys := make([]agentIdentity, 0, count)
	for range count {
		blob, err := agentString(&data)
		if err != nil || len(blob) == 0 {
			return nil, errors.New("invalid SSH agent key")
		}
		comment, err := agentString(&data)
		if err != nil {
			return nil, err
		}
		keys = append(keys, agentIdentity{blob, comment})
	}
	if len(data) != 0 {
		return nil, errors.New("invalid SSH agent keys response")
	}
	return keys, nil
}

func (f *SSHAgentForwarder) check(id string) error {
	if !filepath.IsAbs(f.Socket) || filepath.Clean(f.Socket) != f.Socket || f.Socket == SSHAgentSocket() || f.Socket == DefaultSocket() {
		return errors.New("configure ssh_agent_socket on your Approver Mac with an absolute upstream SSH-agent socket path")
	}
	agents, err := f.agents()
	if err != nil {
		return err
	}
	if agents[id] == "" {
		return errors.New("agent_authentication_required")
	}
	return nil
}

func (f *SSHAgentForwarder) forward(ctx context.Context, id string, request []byte) ([]byte, error) {
	err := f.check(id)
	if err != nil {
		return nil, err
	}
	if bytes.Equal(request, []byte{agentIdentities}) {
		response, err := agentRoundTrip(ctx, f.Socket, request)
		if err != nil {
			return nil, err
		}
		if _, err := parseAgentIdentities(response); err != nil {
			return nil, err
		}
		return response, f.check(id)
	}
	if len(request) == 0 || request[0] != agentSign {
		return []byte{agentFailure}, nil
	}
	data := request[1:]
	_, err = agentString(&data)
	if err != nil {
		return []byte{agentFailure}, nil
	}
	if _, err := agentString(&data); err != nil || len(data) != 4 {
		return []byte{agentFailure}, nil
	}
	flags := binary.BigEndian.Uint32(data)
	if flags != 0 && flags != 2 && flags != 4 {
		return []byte{agentFailure}, nil
	}
	response, err := agentRoundTrip(ctx, f.Socket, request)
	if err != nil {
		return nil, err
	}
	if err := f.check(id); err != nil {
		return nil, err
	}
	if bytes.Equal(response, []byte{agentFailure}) {
		return response, nil
	}
	if len(response) == 0 || response[0] != agentSignAnswer {
		return nil, errors.New("invalid SSH agent signing response")
	}
	data = response[1:]
	signature, err := agentString(&data)
	if err != nil || len(signature) == 0 || len(data) != 0 {
		return nil, errors.New("invalid SSH agent signature")
	}
	return response, nil
}

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
