package passlet

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"time"
)

type savedConnection struct {
	Endpoint string `json:"endpoint"`
	Approver string `json:"approver"`
}

func connectionPath() string { return filepath.Join(StateDir(), "connection.json") }
func readConnection() (savedConnection, error) {
	var c savedConnection
	data, err := os.ReadFile(connectionPath())
	if err != nil {
		return c, err
	}
	if len(data) > 8192 || strictJSON(data, &c) != nil {
		return c, errors.New("invalid saved pairing; run `passlet pair` again")
	}
	if _, err := endpointURL(c.Endpoint); err != nil {
		return c, errors.New("invalid saved pairing endpoint")
	}
	public, err := base64.RawURLEncoding.DecodeString(c.Approver)
	if err != nil || len(public) != 32 {
		return c, errors.New("invalid saved Approver key")
	}
	return c, nil
}

func PairAgent(ctx context.Context, endpoint, code, name string) error {
	public, secret, err := decodeInvitation(code)
	if err != nil {
		return err
	}
	u, err := endpointURL(endpoint)
	if err != nil {
		return err
	}
	private, err := LoadOrCreatePairingKey(filepath.Join(StateDir(), "agent.pem"), filepath.Join(StateDir(), "agent.pub"))
	if err != nil {
		return err
	}
	config, _ := pairedTLS(private, public)
	if name == "" {
		current, err := user.Current()
		if err != nil { // coverage-ignore: Requires the OS account lookup to fail for the running user.
			return err
		}
		host, err := os.Hostname()
		if err != nil { // coverage-ignore: Requires the operating system hostname syscall to fail.
			return err
		}
		name = safeLabel(current.Username + "@" + host)
	}
	transport := &http.Transport{DialTLSContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if u.Scheme == "unix" {
			network, address = "unix", u.Path
		}
		conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		secured := tls.Client(conn, config)
		handshakeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		if err := secured.HandshakeContext(handshakeCtx); err != nil {
			_ = conn.Close()
			return nil, err
		}
		return secured, nil
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 125 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	data, _ := json.Marshal(enrollmentRequest{Secret: base64.RawURLEncoding.EncodeToString(secret), Name: name})
	address, _ := EndpointURL(endpoint, "/v1/pair")
	req, err := http.NewRequestWithContext(ctx, "POST", address, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := client.Do(req)
	if err != nil {
		return errors.New("could not pair with Approver; check the endpoint and pairing code")
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(response.Body, 2049))
	if err != nil {
		return err
	}
	if response.StatusCode != http.StatusOK {
		return errors.New("pairing failed or was denied; create a new code with `passlet approver pair`")
	}
	var result struct {
		Paired bool `json:"paired"`
	}
	if len(body) > 2048 || strictJSON(body, &result) != nil || !result.Paired {
		return errors.New("invalid pairing response")
	}
	data, _ = json.Marshal(savedConnection{Endpoint: endpoint, Approver: base64.RawURLEncoding.EncodeToString(public)})
	return writePairingKey(connectionPath(), append(data, '\n'), true)
}
