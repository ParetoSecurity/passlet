package passlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
)

var cloudflareID = regexp.MustCompile(`^[a-f0-9]{32}$`)
var cloudflarePermission = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)

func (r *Request) validateCloudflare() error {
	if r.Organization != "" || len(r.Repositories) != 0 || (r.Account == "") == (len(r.Zones) == 0) || len(r.Zones) > 3 || len(r.Permissions) < 1 || len(r.Permissions) > 4 {
		return errors.New("cloudflare_requires_account_or_zones_and_permissions")
	}
	if r.Account != "" && !cloudflareID.MatchString(r.Account) {
		return errors.New("invalid_cloudflare_account")
	}
	seen := map[string]bool{}
	for _, zone := range r.Zones {
		if !cloudflareID.MatchString(zone) || seen[zone] {
			return errors.New("invalid_cloudflare_zone")
		}
		seen[zone] = true
	}
	for name, level := range r.Permissions {
		if !cloudflarePermission.MatchString(name) || (level != "read" && level != "write" && level != "run" && level != "revoke") || strings.Contains(name, "api_tokens") {
			return errors.New("invalid_cloudflare_permission")
		}
	}
	sort.Strings(r.Zones)
	return nil
}

func (r Request) cloudflarePrompt(c Caller) string {
	catalog := loadCloudflareCatalog()
	agent := r.Agent
	if agent == "" {
		agent = "Shell"
	}
	location := c.displayName()
	if c.Transport == "unix" {
		location = "local"
	}
	lines := []string{fmt.Sprintf("grant access to Cloudflare for:\n• %s from %s\n• 1 hour", agent, location), ""}
	if r.Account != "" {
		lines = append(lines, "Account: "+cloudflareResourceLabel(r.Account, catalog.Accounts))
	} else {
		lines = append(lines, "Zones:")
		for _, zone := range r.Zones {
			lines = append(lines, "• "+cloudflareResourceLabel(zone, catalog.Zones))
		}
	}
	lines = append(lines, "", "Permissions:")
	for _, name := range slices.Sorted(maps.Keys(r.Permissions)) {
		lines = append(lines, "• "+name+": "+r.Permissions[name])
	}
	return strings.Join(lines, "\n")
}

type Cloudflare struct {
	HTTP *http.Client
	API  string
}

func NewCloudflare() *Cloudflare {
	return &Cloudflare{HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, API: "https://api.cloudflare.com/client/v4"}
}

type cloudflareGroup struct {
	ID     string   `json:"id"`
	Name   string   `json:"name,omitempty"`
	Scopes []string `json:"scopes,omitempty"`
}
type cloudflarePolicy struct {
	Effect    string            `json:"effect"`
	Resources map[string]string `json:"resources"`
	Groups    []cloudflareGroup `json:"permission_groups"`
}
type cloudflareToken struct {
	ID        string             `json:"id"`
	Value     string             `json:"value"`
	Status    string             `json:"status"`
	ExpiresAt time.Time          `json:"expires_on"`
	Policies  []cloudflarePolicy `json:"policies"`
}

func (c *Cloudflare) call(ctx context.Context, token, method, path string, body, result any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return errors.New("invalid_cloudflare_request")
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, c.API+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("invalid_cloudflare_endpoint")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return errors.New("cloudflare_unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("cloudflare_http_%d", resp.StatusCode)
	}
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 1048577))
	if err != nil || len(data) > 1048576 || json.Unmarshal(data, &envelope) != nil || !envelope.Success || json.Unmarshal(envelope.Result, result) != nil {
		return errors.New("invalid_cloudflare_response")
	}
	return nil
}

func (c *Cloudflare) Verify(ctx context.Context, token string) (Credentials, error) {
	var verified cloudflareToken
	if err := c.call(ctx, token, "GET", "/user/tokens/verify", nil, &verified); err != nil {
		return Credentials{}, err
	}
	if !cloudflareID.MatchString(verified.ID) || verified.Status != "active" || (!verified.ExpiresAt.IsZero() && !verified.ExpiresAt.After(time.Now())) {
		return Credentials{}, errors.New("cloudflare_token_inactive")
	}
	var details cloudflareToken
	if err := c.call(ctx, token, "GET", "/user/tokens/"+verified.ID, nil, &details); err != nil {
		return Credentials{}, err
	}
	var groups []cloudflareGroup
	if err := c.call(ctx, token, "GET", "/user/tokens/permission_groups", nil, &groups); err != nil {
		return Credentials{}, err
	}
	for _, policy := range details.Policies {
		if policy.Effect != "allow" {
			continue
		}
		for _, granted := range policy.Groups {
			for _, group := range groups {
				if granted.ID == group.ID && (group.Name == "API Tokens Write" || group.Name == "API Tokens Edit") {
					return Credentials{Provider: "cloudflare", AccessToken: token, CloudflareTokenID: verified.ID}, nil
				}
			}
		}
	}
	return Credentials{}, errors.New("cloudflare_requires_create_additional_tokens_template")
}

func cloudflarePermissionKey(name string) (string, string) {
	index := strings.LastIndex(name, " ")
	if index < 0 {
		return "", ""
	}
	level := strings.ToLower(name[index+1:])
	if level == "edit" {
		level = "write"
	}
	base := strings.Trim(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(name[:index]), "_"), "_")
	return base, level
}

func (c *Cloudflare) Mint(ctx context.Context, credentials Credentials, r Request) (Grant, error) {
	if err := r.Validate(); err != nil {
		return Grant{}, err
	}
	if r.Provider != "cloudflare" || credentials.Provider != "cloudflare" || credentials.Validate() != nil {
		return Grant{}, errors.New("invalid_cloudflare_credentials")
	}
	var groups []cloudflareGroup
	if err := c.call(ctx, credentials.AccessToken, "GET", "/user/tokens/permission_groups", nil, &groups); err != nil {
		return Grant{}, err
	}
	scope := "com.cloudflare.api.account"
	resources := map[string]string{}
	if r.Account != "" {
		resources[scope+"."+r.Account] = "*"
	} else {
		scope += ".zone"
		for _, zone := range r.Zones {
			resources[scope+"."+zone] = "*"
		}
	}
	selected := []cloudflareGroup{}
	for _, name := range slices.Sorted(maps.Keys(r.Permissions)) {
		ids := map[string]bool{}
		for _, group := range groups {
			key, level := cloudflarePermissionKey(group.Name)
			if key == name && level == r.Permissions[name] && slices.Contains(group.Scopes, scope) && cloudflareID.MatchString(group.ID) {
				ids[group.ID] = true
			}
		}
		if len(ids) != 1 {
			return Grant{}, errors.New("unknown_or_ambiguous_cloudflare_permission")
		}
		for id := range ids {
			selected = append(selected, cloudflareGroup{ID: id})
		}
	}
	policies := []cloudflarePolicy{{Effect: "allow", Resources: resources, Groups: selected}}
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	body := struct {
		Name      string             `json:"name"`
		Policies  []cloudflarePolicy `json:"policies"`
		ExpiresAt time.Time          `json:"expires_on"`
	}{"Passlet", policies, expiry}
	var issued cloudflareToken
	if err := c.call(ctx, credentials.AccessToken, "POST", "/user/tokens", body, &issued); err != nil {
		return Grant{}, err
	}
	if !cloudflareID.MatchString(issued.ID) || issued.Value == "" || issued.Status != "active" || !issued.ExpiresAt.After(time.Now()) || issued.ExpiresAt.After(expiry) || !sameCloudflarePolicies(issued.Policies, policies) {
		return Grant{}, errors.New("unexpected_cloudflare_token_scope_or_expiry")
	}
	return Grant{Provider: "cloudflare", Token: issued.Value, ExpiresAt: issued.ExpiresAt, Account: r.Account, Zones: r.Zones, Permissions: r.Permissions, CloudflareTokenID: issued.ID}, nil
}

func sameCloudflarePolicies(actual, expected []cloudflarePolicy) bool {
	if len(actual) != 1 || actual[0].Effect != "allow" || !maps.Equal(actual[0].Resources, expected[0].Resources) || len(actual[0].Groups) != len(expected[0].Groups) {
		return false
	}
	ids := map[string]bool{}
	for _, group := range actual[0].Groups {
		ids[group.ID] = true
	}
	for _, group := range expected[0].Groups {
		if !ids[group.ID] {
			return false
		}
	}
	return true
}
