package passlet

import (
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
		{"approval cannot fit", []string{strings.Repeat("o", 100) + "/" + strings.Repeat("a", 100), strings.Repeat("o", 100) + "/" + strings.Repeat("b", 100), strings.Repeat("o", 100) + "/" + strings.Repeat("c", 100)}, map[string]string{"actions": "read", "contents": "read", "issues": "read", "pull_requests": "read", "checks": "read", "deployments": "read", "discussions": "read", "environments": "read"}, "request_too_large_for_prompt"},
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
