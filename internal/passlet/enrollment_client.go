package passlet

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
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
		name, err = localAgentName()
		if err != nil { // coverage-ignore: Requires OS account or hostname lookup failure.
			return err
		}
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
	if err := routeThroughProxy(transport, u, func() (*tls.Config, error) { return config, nil }); err != nil {
		return err
	}
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
		if problem := SandboxNetworkProblem(err); problem != "" {
			return fmt.Errorf("%s", problem)
		}
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

func localAgentName() (string, error) {
	current, err := user.Current()
	if err != nil { // coverage-ignore: Requires the OS account lookup to fail for the running user.
		return "", err
	}
	host, err := os.Hostname()
	if err != nil { // coverage-ignore: Requires the operating system hostname syscall to fail.
		return "", err
	}
	return safeLabel(current.Username + "@" + host), nil
}

func PairLocalAgent() error {
	name, err := localAgentName()
	if err != nil { // coverage-ignore: Requires OS account or hostname lookup failure.
		return err
	}
	agent, err := LoadOrCreatePairingKey(filepath.Join(StateDir(), "agent.pem"), filepath.Join(StateDir(), "agent.pub"))
	if err != nil {
		return err
	}
	approver, err := LoadOrCreatePairingKey(filepath.Join(StateDir(), "approver.pem"), filepath.Join(StateDir(), "approver.pub"))
	if err != nil {
		return err
	}
	key := agent.Public().(ed25519.PublicKey)
	if err := withPairingState(true, func(s *pairingState) error {
		if s.Revoked[keyID(key)] {
			return errors.New("local Agent was revoked; pair it explicitly with `passlet approver pair` and `passlet pair`")
		}
		for id, a := range s.Agents {
			if a.Name == name && id != keyID(key) {
				return errors.New("local Agent name already paired; revoke it before replacing it")
			}
		}
		s.Agents[keyID(key)] = pairedAgent{Name: name, PublicKey: base64.RawURLEncoding.EncodeToString(key)}
		return nil
	}); err != nil {
		return err
	}
	data, _ := json.Marshal(savedConnection{Endpoint: "unix://" + DefaultSocket(), Approver: base64.RawURLEncoding.EncodeToString(approver.Public().(ed25519.PublicKey))})
	return writePairingKey(connectionPath(), append(data, '\n'), true)
}
