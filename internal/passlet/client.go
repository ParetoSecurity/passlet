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
	"time"
)

func RequestGrant(ctx context.Context, endpoint string, r Request) (Grant, error) {
	transport := &http.Transport{Proxy: nil, DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
		ctx, cancel := context.WithTimeout(ctx, ConnectDeadline)
		defer cancel()
		return (&tls.Dialer{NetDialer: &net.Dialer{Timeout: ConnectDeadline}}).DialContext(ctx, network, addr)
	}, ResponseHeaderTimeout: ClientDeadline}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: ClientDeadline, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return requestGrant(ctx, endpoint, r, client)
}

func requestGrant(ctx context.Context, endpoint string, r Request, client *http.Client) (Grant, error) {
	if err := r.Validate(); err != nil {
		return Grant{}, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return Grant{}, errors.New("endpoint_must_be_https_origin")
	}
	u.Path = "/v1/grants"
	data, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, "POST", u.String(), bytes.NewReader(data))
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
