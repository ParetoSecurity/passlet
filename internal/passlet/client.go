package passlet

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"time"
)

func RequestGrant(ctx context.Context, endpoint string, r Request) (Grant, error) {
	client, close, err := EndpointClient(endpoint)
	if err != nil {
		return Grant{}, err
	}
	defer close()
	return requestGrant(ctx, endpoint, r, client)
}

func endpointURL(endpoint string) (*url.URL, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" {
		return nil, errors.New("endpoint_must_be_https_origin_or_absolute_unix_socket")
	}
	if u.Scheme == "unix" && u.Host == "" && filepath.IsAbs(u.Path) && filepath.Clean(u.Path) == u.Path && len(u.Path) <= 103 && u.Path != "/" {
		return u, nil
	}
	if u.Scheme == "https" && u.Host != "" && (u.Path == "" || u.Path == "/") {
		return u, nil
	}
	return nil, errors.New("endpoint_must_be_https_origin_or_absolute_unix_socket")
}

// EndpointClient never uses ambient proxies, follows redirects, or falls back
// from a Unix socket to a network endpoint.
func EndpointClient(endpoint string) (*http.Client, func(), error) {
	u, err := endpointURL(endpoint)
	if err != nil {
		return nil, nil, err
	}
	transport := &http.Transport{Proxy: nil, DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, ConnectDeadline)
		defer cancel()
		return (&tls.Dialer{NetDialer: &net.Dialer{Timeout: ConnectDeadline}}).DialContext(ctx, network, addr)
	}, ResponseHeaderTimeout: ClientDeadline}
	if u.Scheme == "unix" {
		transport.DialContext = func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{Timeout: ConnectDeadline}).DialContext(ctx, "unix", u.Path)
		}
	}
	client := &http.Client{Transport: transport, Timeout: ClientDeadline, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, transport.CloseIdleConnections, nil
}

func EndpointURL(endpoint, path string) (string, error) {
	u, err := endpointURL(endpoint)
	if err != nil {
		return "", err
	}
	if u.Scheme == "unix" {
		u = &url.URL{Scheme: "http", Host: "passlet.local"}
	}
	u.Path = path
	return u.String(), nil
}

func requestGrant(ctx context.Context, endpoint string, r Request, client *http.Client) (Grant, error) {
	if err := r.Validate(); err != nil {
		return Grant{}, err
	}
	target, err := EndpointURL(endpoint, "/v1/grants")
	if err != nil {
		return Grant{}, err
	}
	data, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, "POST", target, bytes.NewReader(data))
	if err != nil {
		return Grant{}, errors.New("invalid_endpoint")
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return Grant{}, errors.New("request_cancelled")
		}
		return Grant{}, errors.New("approver_unavailable_or_connection_lost")
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 65537))
	if err != nil || len(payload) > 65536 {
		return Grant{}, errors.New("invalid_response")
	}
	if resp.StatusCode != 200 {
		var problem struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(payload, &problem) == nil {
			switch problem.Error {
			case "github_http_403":
				return Grant{}, errors.New("github_access_denied_check_App_permissions_installation_approval_and_org_policy")
			case "github_http_401":
				return Grant{}, errors.New("reauthorization_required")
			case "approval_denied", "approval_cancelled", "approval_timeout", "approver_busy", "identity_unavailable", "invalid_request", "credentials_missing", "repository_not_installed", "audit_unavailable", "reauthorization_required":
				return Grant{}, errors.New(problem.Error)
			}
		}
		return Grant{}, errors.New("grant_failed")
	}
	var grant Grant
	if strictJSON(payload, &grant) != nil || grant.Token == "" || grant.ID == "" || grant.User == "" || !grant.ExpiresAt.After(time.Now()) || time.Until(grant.ExpiresAt) > 8*time.Hour+time.Minute {
		return Grant{}, errors.New("invalid_grant")
	}
	if len(grant.Repositories) != len(r.Repositories) || len(grant.Permissions) != len(r.Permissions) {
		return Grant{}, errors.New("unexpected_grant_scope")
	}
	for i, repo := range r.Repositories {
		if grant.Repositories[i] != repo {
			return Grant{}, errors.New("unexpected_grant_scope")
		}
	}
	for k, v := range r.Permissions {
		if grant.Permissions[k] != v {
			return Grant{}, errors.New("unexpected_grant_scope")
		}
	}
	return grant, nil
}
