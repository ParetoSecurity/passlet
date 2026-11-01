package passlet

import (
	"strings"
	"testing"
)

func TestPermissionScopesAndLevels(t *testing.T) {
	for name, spec := range permissionCatalog {
		for _, level := range spec.Levels {
			r := Request{Permissions: map[string]string{name: level}}
			switch spec.Scope {
			case "repository":
				r.Repositories = []string{"alice/repo"}
			case "organization":
				r.Organization = "acme"
			case "account":
				r.Account = "alice"
			default:
				t.Fatal("unknown scope", name)
			}
			if err := r.Validate(); err != nil {
				t.Errorf("%s:%s: %v", name, level, err)
			}
			if !strings.Contains(r.Prompt(Caller{Name: "agent"}), name+": "+level) {
				t.Error("permission missing from prompt", name)
			}
		}
	}
	for _, r := range []Request{
		{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"members": "write"}},
		{Organization: "acme", Permissions: map[string]string{"contents": "write"}},
		{Organization: "acme", Permissions: map[string]string{"git_ssh_keys": "write"}},
		{Account: "alice", Permissions: map[string]string{"organization_secrets": "write"}},
		{Organization: "acme", Repositories: []string{"acme/repo"}, Permissions: map[string]string{"members": "write"}},
		{Organization: "acme", Account: "alice", Permissions: map[string]string{"members": "write"}},
		{Account: "alice\nother", Permissions: map[string]string{"git_ssh_keys": "write"}},
		{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"administration": "admin"}},
		{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"single_file": "write"}},
	} {
		if r.Validate() == nil {
			t.Error("accepted scope confusion or invalid level", r)
		}
	}
}

func TestWidePromptsDeclareRepositoryMetadata(t *testing.T) {
	for _, r := range []Request{
		{Organization: "Acme", Permissions: map[string]string{"members": "write"}},
		{Account: "Alice", Permissions: map[string]string{"git_ssh_keys": "write"}},
	} {
		if err := r.Validate(); err != nil {
			t.Fatal(err)
		}
		prompt := r.Prompt(Caller{Name: "agent"})
		if !strings.Contains(prompt, "All installed repositories: metadata read only") || !strings.Contains(prompt, "-wide access") {
			t.Fatal("wide scope not disclosed", prompt)
		}
	}
}

func TestNormalizeRegistrationPermissions(t *testing.T) {
	got, err := normalizeRegistrationPermissions(map[string]string{"emails": "write", "keys": "read", "contents": "write"})
	if err != nil || got["email_addresses"] != "write" || got["git_ssh_keys"] != "read" || got["contents"] != "write" || len(got) != 3 {
		t.Fatalf("unexpected normalization: %v, %v", got, err)
	}
	for _, names := range [][2]string{{"emails", "email_addresses"}, {"keys", "git_ssh_keys"}} {
		if _, err := normalizeRegistrationPermissions(map[string]string{names[0]: "write", names[1]: "write"}); err == nil {
			t.Fatal("accepted ambiguous duplicate permission")
		}
	}
}
