package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
)

func SandboxNetworkProblem(err error) string {
	var connection *net.OpError
	if os.Getenv("CODEX_SANDBOX_NETWORK_DISABLED") == "1" &&
		errors.As(err, &connection) && connection.Op == "dial" && errors.Is(err, os.ErrPermission) {
		return "The Codex sandbox denied this connection. Allow access to the endpoint or approve running `passlet` outside the sandbox."
	}
	return ""
}

type healthConnectionError struct{ cause error }

func (e healthConnectionError) Error() string {
	if problem := SandboxNetworkProblem(e.cause); problem != "" {
		return problem
	}
	return "approver_unavailable_check_service_or_SSH_tunnel"
}
func (e healthConnectionError) Unwrap() error { return e.cause }

func Health(ctx context.Context, endpoint string) error {
	client, close, err := EndpointClient(endpoint)
	if err != nil {
		return err
	}
	defer close()
	ctx, cancel := context.WithTimeout(ctx, ConnectDeadline)
	defer cancel()
	target, _ := EndpointURL(endpoint, "/v1/health")
	req, _ := http.NewRequestWithContext(ctx, "GET", target, nil)
	resp, err := client.Do(req)
	if err != nil {
		return healthConnectionError{cause: err}
	}
	defer func() { _ = resp.Body.Close() }()
	var health struct {
		Status string `json:"status"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 1024)).Decode(&health) != nil || health.Status != "ready" {
		return errors.New("approver_health_check_failed")
	}
	return nil
}
