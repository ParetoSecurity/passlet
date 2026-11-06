package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

const tailscalePort = "17400"

const tailscaleBackend = "127.0.0.1:17401"
const tailscaleTarget = "tcp://" + tailscaleBackend

type tailscaleStatus struct {
	Version      string
	BackendState string
	Self         struct{ DNSName string }
}

type tailscaleServeConfig struct {
	TCP map[string]struct {
		HTTPS         bool
		HTTP          bool
		TCPForward    string
		TerminateTLS  string
		ProxyProtocol int
	}
	Web map[string]struct {
		Handlers map[string]struct {
			Proxy         string
			Path          string
			Text          string
			AcceptAppCaps []string
		}
	}
	AllowFunnel map[string]bool
	Foreground  map[string]tailscaleServeConfig
}

func tailscaleNode(ctx context.Context, command overviewCommand) (tailscaleStatus, error) {
	data, err := command(ctx, "tailscale", "status", "--json")
	if err != nil {
		return tailscaleStatus{}, fmt.Errorf("cannot query Tailscale; check that it is installed and running: %w", err)
	}
	var status tailscaleStatus
	if json.Unmarshal(data, &status) != nil || status.BackendState != "Running" || status.Self.DNSName == "" {
		return status, errors.New("tailscale_not_running_or_missing_hostname")
	}
	status.Self.DNSName = strings.TrimSuffix(status.Self.DNSName, ".")
	return status, nil
}

func readTailscaleServe(ctx context.Context, command overviewCommand) (tailscaleServeConfig, error) {
	data, err := command(ctx, "tailscale", "serve", "status", "--json")
	if err != nil {
		return tailscaleServeConfig{}, fmt.Errorf("cannot read Tailscale Serve configuration: %w", err)
	}
	var config tailscaleServeConfig
	if json.Unmarshal(data, &config) != nil {
		return config, errors.New("invalid_tailscale_serve_configuration")
	}
	return config, nil
}

func (c tailscaleServeConfig) occupiesPort() bool {
	if _, ok := c.TCP[tailscalePort]; ok {
		return true
	}
	for host := range c.Web {
		if strings.HasSuffix(host, ":"+tailscalePort) {
			return true
		}
	}
	for host, enabled := range c.AllowFunnel {
		if enabled && strings.HasSuffix(host, ":"+tailscalePort) {
			return true
		}
	}
	for _, config := range c.Foreground {
		if config.occupiesPort() {
			return true
		}
	}
	return false
}

func (c tailscaleServeConfig) forwardsTo(hostname, target string) bool {
	hostPort := net.JoinHostPort(hostname, tailscalePort)
	tcp, ok := c.TCP[tailscalePort]
	if !ok || tcp.HTTPS || tcp.HTTP || tcp.TerminateTLS != "" || tcp.ProxyProtocol != 0 ||
		tcp.TCPForward != strings.TrimPrefix(target, "tcp://") || c.AllowFunnel[hostPort] {
		return false
	}
	for name := range c.Web {
		if strings.HasSuffix(name, ":"+tailscalePort) {
			return false
		}
	}
	for _, config := range c.Foreground {
		if config.occupiesPort() {
			return false
		}
	}
	return true
}

func ensureTailscale(ctx context.Context, target string, command overviewCommand) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	status, err := tailscaleNode(ctx, command)
	if err != nil {
		return err
	}
	config, err := readTailscaleServe(ctx, command)
	if err != nil {
		return err
	}
	if config.forwardsTo(status.Self.DNSName, target) {
		return nil
	}
	if config.occupiesPort() {
		return errors.New("tailscale Serve port 17400 is already configured; resolve the conflict before enabling Passlet")
	}
	if _, err := command(ctx, "tailscale", "serve", "--bg", "--tcp="+tailscalePort, target); err != nil {
		return fmt.Errorf("cannot configure Tailscale Serve; check Tailscale Serve permissions: %w", err)
	}
	config, err = readTailscaleServe(ctx, command)
	if err != nil {
		return err
	}
	if !config.forwardsTo(status.Self.DNSName, target) {
		return errors.New("tailscale_forwarding_not_verified")
	}
	return nil
}
