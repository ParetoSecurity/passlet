package passlet

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCredentialsRequireEveryField(t *testing.T) {
	valid := Credentials{ClientID: "id", ClientSecret: "secret", AccessToken: "access", RefreshToken: "refresh", ExpiresAt: time.Now().Add(time.Hour)}
	for _, field := range []string{"client ID", "client secret", "access token", "refresh token", "expiry"} {
		t.Run(field, func(t *testing.T) {
			c := valid
			switch field {
			case "client ID":
				c.ClientID = ""
			case "client secret":
				c.ClientSecret = ""
			case "access token":
				c.AccessToken = ""
			case "refresh token":
				c.RefreshToken = ""
			case "expiry":
				c.ExpiresAt = time.Time{}
			}
			if err := c.Validate(); err == nil || err.Error() != "incomplete_credentials" {
				t.Fatalf("missing %s: %v", field, err)
			}
		})
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRequestRejectsAmbiguousAndOversizedScopes(t *testing.T) {
	for _, tc := range []struct {
		name        string
		repos       []string
		permissions map[string]string
		want        string
	}{
		{"malformed repository", []string{"alice"}, map[string]string{"contents": "read"}, "invalid_repository"},
		{"duplicate ignoring case", []string{"alice/repo", "Alice/Repo"}, map[string]string{"contents": "read"}, "duplicate_repository"},
		{"metadata exceeds permission budget", []string{"alice/repo"}, map[string]string{"actions": "read", "contents": "read", "issues": "read", "pull_requests": "read", "checks": "read", "deployments": "read", "discussions": "read", "environments": "read", "packages": "read"}, "invalid_request"},
		{"too many repositories", make([]string, 101), map[string]string{"contents": "read"}, "invalid_request"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := Request{Repositories: tc.repos, Permissions: tc.permissions}
			if err := r.Validate(); err == nil || err.Error() != tc.want {
				t.Fatalf("want %s, got %v", tc.want, err)
			}
		})
	}
}

func TestCallerLabelRemovesControlCharactersAndTruncates(t *testing.T) {
	input := "\x1b\n\t" + strings.Repeat("x", 101) + "é"
	if got := safeLabel(input); got != strings.Repeat("x", 100) {
		t.Fatalf("unsafe label: %q", got)
	}
}

func TestStrictJSONRejectsClosingDelimiterAfterObject(t *testing.T) {
	var value map[string]string
	if err := strictJSON([]byte(`{"value":"ok"}]`), &value); err == nil {
		t.Fatal("accepted trailing closing delimiter")
	}
}

func TestLargeRepositoryPromptPreservesOwnerPermissionsAndCount(t *testing.T) {
	owner := strings.Repeat("o", 100)
	r := Request{Permissions: map[string]string{"metadata": "read"}}
	for _, name := range []string{"repository_custom_properties", "secret_scanning_alerts", "vulnerability_alerts", "repository_projects", "dependabot_secrets", "artifact_metadata", "repository_hooks", "security_events"} {
		spec := permissionCatalog[name]
		r.Permissions[name] = spec.Levels[len(spec.Levels)-1]
	}
	for i := 0; i < 100; i++ {
		r.Repositories = append(r.Repositories, fmt.Sprintf("%s/%096d", owner, i))
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	before := append([]string(nil), r.Repositories...)
	prompt := r.Prompt(Caller{Name: strings.Repeat("c", 100)})
	if len(prompt) > 650 || !strings.Contains(prompt, "Owner: "+owner) {
		t.Fatalf("invalid approval prompt: %s", prompt)
	}
	shown := strings.Count(prompt, "• "+owner+"/")
	if !strings.Contains(prompt, fmt.Sprintf("and %d additional repositories\n\nPermissions:", 100-shown)) {
		t.Fatalf("incorrect repository count: %s", prompt)
	}
	for name, level := range r.Permissions {
		if name != "metadata" && !strings.Contains(prompt+"\n", "• "+name+": "+level+"\n") {
			t.Fatalf("missing permission %s", name)
		}
	}
	if !reflect.DeepEqual(before, r.Repositories) {
		t.Fatal("display changed repository scope")
	}
}

func TestApprovalUsesTrustedLabelWithoutChangingAuditIdentity(t *testing.T) {
	for _, tc := range []struct{ transport, name, want string }{
		{"paired", "alice@cruncher", "alice@cruncher"},
		{"tailscale", "cruncher", "cruncher"},
		{"paired", "cruncher\n", "cruncher"},
		{"unix", "local/SSH socket client", "local"},
		{"ssh", "cruncher", "cruncher"},
	} {
		caller := Caller{Transport: tc.transport, Name: tc.name, NodeID: "node"}
		prompt := (Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}).Prompt(caller)
		if !strings.HasPrefix(prompt, "grant access to GitHub for:\n• Shell from "+tc.want+"\n") {
			t.Fatal(prompt)
		}
		if caller.Name != tc.name || caller.NodeID != "node" {
			t.Fatal("display altered audit identity")
		}
	}
}

func TestAgentLabelValidation(t *testing.T) {
	for _, label := range []string{"Claude\nPermissions: admin", " Claude", "Claude ", "Cl\x00aude", strings.Repeat("a", 41)} {
		r := Request{Agent: label, Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read"}}
		if err := r.Validate(); err == nil || err.Error() != "invalid_agent_label" {
			t.Fatalf("accepted invalid app label %q: %v", label, err)
		}
	}
}

func TestLabeledPromptPreservesOwnerWhenRepositoriesAreTruncated(t *testing.T) {
	r := Request{Agent: "Claude", Permissions: map[string]string{"contents": "read"}}
	for i := 0; i < 100; i++ {
		r.Repositories = append(r.Repositories, fmt.Sprintf("alice/repository-%03d", i))
	}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	prompt := r.Prompt(Caller{Transport: "ssh", Name: "cruncher"})
	if len(prompt) > 650 || !strings.Contains(prompt, "• alice/repository-") || strings.Contains(prompt, "Owner: alice") || !strings.Contains(prompt, "additional repositories") || !strings.Contains(prompt, "• contents: read") {
		t.Fatalf("incomplete truncated prompt: %s", prompt)
	}
}

func TestMetadataOnlyPromptOmitsEmptyPermissions(t *testing.T) {
	r := Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"metadata": "read"}}
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	prompt := r.Prompt(Caller{Transport: "unix"})
	if strings.Contains(prompt, "Permissions:") || !strings.HasSuffix(prompt, "• alice/repo") || r.Permissions["metadata"] != "read" {
		t.Fatalf("unexpected baseline permission prompt: %q", prompt)
	}
}
