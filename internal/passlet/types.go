package passlet

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"time"
)

const ServerDeadline = 120 * time.Second
const ClientDeadline = 130 * time.Second
const ConnectDeadline = 5 * time.Second

type Request struct {
	Organization string            `json:"organization,omitempty"`
	Account      string            `json:"account,omitempty"`
	Repositories []string          `json:"repositories"`
	Permissions  map[string]string `json:"permissions"`
}
type Caller struct {
	Transport string   `json:"transport"`
	NodeID    string   `json:"node_id"`
	Name      string   `json:"name"`
	User      string   `json:"user,omitempty"`
	Tags      []string `json:"tags,omitempty"`
}
type Grant struct {
	Organization string            `json:"organization,omitempty"`
	Account      string            `json:"account,omitempty"`
	ID           string            `json:"grant_id"`
	Token        string            `json:"token"`
	ExpiresAt    time.Time         `json:"expires_at"`
	Repositories []string          `json:"repositories"`
	Permissions  map[string]string `json:"permissions"`
	User         string            `json:"user"`
}
type Credentials struct {
	Account      AccountInfo `json:"account"`
	ClientID     string      `json:"client_id"`
	ClientSecret string      `json:"client_secret"`
	AccessToken  string      `json:"access_token"`
	RefreshToken string      `json:"refresh_token"`
	ExpiresAt    time.Time   `json:"expires_at"`
}

func (c Credentials) Validate() error {
	if c.ClientID == "" || c.ClientSecret == "" || c.AccessToken == "" || c.RefreshToken == "" || c.ExpiresAt.IsZero() {
		return errors.New("incomplete_credentials")
	}
	return nil
}

var component = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,99}$`)

func (r *Request) Validate() error {
	if len(r.Repositories) > 3 || len(r.Permissions) < 1 || len(r.Permissions) > 9 {
		return errors.New("invalid_request")
	}
	scope := "repository"
	if r.Organization != "" {
		scope = "organization"
	}
	if r.Account != "" {
		scope = "account"
	}
	if (r.Organization != "" && r.Account != "") || (scope != "repository" && len(r.Repositories) != 0) || (scope == "repository" && len(r.Repositories) == 0) {
		return errors.New("choose_one_scope_repositories_organization_or_account")
	}
	if scope != "repository" {
		target := r.Organization
		if scope == "account" {
			target = r.Account
		}
		if !githubHandle.MatchString(target) || len(target) > 39 {
			return errors.New("invalid_scope_target")
		}
		r.Organization = strings.ToLower(r.Organization)
		r.Account = strings.ToLower(r.Account)
	}
	seen := map[string]bool{}
	owner := ""
	for i, repo := range r.Repositories {
		pieces := strings.Split(repo, "/")
		if len(pieces) != 2 || !component.MatchString(pieces[0]) || !component.MatchString(pieces[1]) {
			return errors.New("invalid_repository")
		}
		repo = strings.ToLower(repo)
		if seen[repo] {
			return errors.New("duplicate_repository")
		}
		seen[repo] = true
		if owner != "" && owner != strings.ToLower(pieces[0]) {
			return errors.New("one_owner_per_request")
		}
		owner = strings.ToLower(pieces[0])
		r.Repositories[i] = repo
	}
	if _, hasMetadata := r.Permissions["metadata"]; !hasMetadata && len(r.Permissions) > 8 {
		return errors.New("invalid_request")
	}
	for k, v := range r.Permissions {
		if !validPermission(k, v) {
			return errors.New("invalid_permission")
		}
	}
	for name := range r.Permissions {
		if name != "metadata" && permissionCatalog[name].Scope != scope {
			return errors.New("permission_requires_" + permissionCatalog[name].Scope + "_scope")
		}
	}
	// Metadata read is intrinsic to GitHub repository tokens and must be displayed.
	r.Permissions["metadata"] = "read"
	sort.Strings(r.Repositories)
	if len(r.Prompt(Caller{Name: "requesting-node"})) > 650 {
		return errors.New("request_too_large_for_prompt")
	}
	return nil
}
func safeLabel(s string) string {
	var b strings.Builder
	for _, c := range s {
		if c >= 32 && c <= 126 {
			b.WriteRune(c)
		}
	}
	s = b.String()
	if len(s) > 100 {
		s = s[:100]
	}
	return s
}
func (r Request) Prompt(c Caller) string {
	keys := make([]string, 0, len(r.Permissions))
	for k := range r.Permissions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	lines := []string{fmt.Sprintf("grant %s GitHub access", safeLabel(c.Name)), "", "Repositories:"}
	if r.Organization != "" {
		lines = []string{fmt.Sprintf("grant %s GitHub access", safeLabel(c.Name)), "", "Organization: " + r.Organization, "Organization-wide access", "All installed repositories: metadata read only"}
	}
	if r.Account != "" {
		lines = []string{fmt.Sprintf("grant %s GitHub access", safeLabel(c.Name)), "", "Account: " + r.Account, "Account-wide access", "All installed repositories: metadata read only"}
	}
	for _, repo := range r.Repositories {
		lines = append(lines, "• "+repo)
	}
	lines = append(lines, "", "Permissions:")
	for _, k := range keys {
		lines = append(lines, "• "+k+": "+r.Permissions[k])
	}
	lines = append(lines, "", "GitHub sets expiry at issuance (approximately 8 hours)")
	return strings.Join(lines, "\n")
}
func strictJSON(data []byte, v any) error {
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid_json")
	}
	if d.More() {
		return errors.New("invalid_json")
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		return errors.New("invalid_json")
	}
	return nil
}
