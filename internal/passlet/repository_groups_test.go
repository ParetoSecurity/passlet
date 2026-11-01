package passlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestRepositoryGroupUsesOneApprovalAndOneScopedToken(t *testing.T) {
	repos := []string{"teamniteo/ops", "teamniteo/gha-actions", "teamniteo/pyramid_niteo", "teamniteo/scrooge", "teamniteo/canario", "teamniteo/pareto-cloud", "teamniteo/grafana", "teamniteo/ebn-nixos", "teamniteo/servers"}
	permissions := map[string]string{"contents": "write", "pull_requests": "write", "workflows": "write", "actions": "read", "vulnerability_alerts": "read", "metadata": "read"}
	for _, name := range []string{"all repositories installed", "one repository not installed", "overflowing list"} {
		missing := name == "one repository not installed"
		t.Run(name, func(t *testing.T) {
			repos := append([]string(nil), repos...)
			if name == "overflowing list" {
				for i := 0; i < 20; i++ {
					repos = append(repos, fmt.Sprintf("teamniteo/project-%02d", i))
				}
			}
			expectedIDs := make([]int, len(repos))
			for i := range expectedIDs {
				expectedIDs[i] = i + 1
			}
			mintCalls := 0
			github := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/user":
					json.NewEncoder(w).Encode(map[string]string{"login": "alice"})
				case "/user/installations":
					json.NewEncoder(w).Encode(map[string]any{"installations": []any{map[string]any{"id": 42, "account": map[string]string{"login": "teamniteo"}}}})
				case "/user/installations/42/repositories":
					installed := []map[string]any{}
					for i, repo := range repos {
						if !missing || i != len(repos)-1 {
							installed = append(installed, map[string]any{"id": i + 1, "full_name": repo})
						}
					}
					// Installation access includes an unrequested repo; it must not reach the grant.
					installed = append(installed, map[string]any{"id": 99, "full_name": "teamniteo/unrequested"})
					json.NewEncoder(w).Encode(map[string]any{"repositories": installed})
				case "/applications/client/token/scoped":
					mintCalls++
					var body struct {
						Target      string            `json:"target"`
						IDs         []int             `json:"repository_ids"`
						Permissions map[string]string `json:"permissions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					sort.Ints(body.IDs)
					if body.Target != "teamniteo" || !reflect.DeepEqual(body.IDs, expectedIDs) || !reflect.DeepEqual(body.Permissions, permissions) {
						t.Errorf("incorrect token scope: %+v", body)
					}
					json.NewEncoder(w).Encode(map[string]any{"token": "fixture-child", "expires_at": time.Now().Add(time.Hour), "user": map[string]string{"login": "alice"}, "installation": map[string]any{"repository_selection": "selected", "permissions": permissions, "account": map[string]string{"login": "teamniteo"}}})
				default:
					t.Errorf("unexpected GitHub call: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer github.Close()
			provider := NewGitHub()
			provider.API = github.URL
			approval := &testApprover{session: &testSession{c: Credentials{ClientID: "client", ClientSecret: "fixture-secret", AccessToken: "fixture-parent", ExpiresAt: time.Now().Add(time.Hour)}}}
			audit := &memoryAudit{}
			server := NewServer(approval, provider, audit)
			server.Identity = func(context.Context, string) (Caller, error) { return Caller{Name: strings.Repeat("a", 100)}, nil }
			httpServer := httptest.NewTLSServer(server)
			defer httpServer.Close()
			grant, err := requestGrant(context.Background(), httpServer.URL, Request{Repositories: append([]string(nil), repos...), Permissions: permissions}, httpServer.Client())
			if len(approval.prompts) != 1 {
				t.Fatalf("expected one approval, got %d", len(approval.prompts))
			}
			prompt := approval.prompts[0]
			for _, repo := range repos {
				if name != "overflowing list" && strings.Count(prompt, "• "+repo+"\n") != 1 {
					t.Fatalf("repository missing or duplicated in approval: %s", repo)
				}
			}
			if name == "overflowing list" {
				shown := strings.Count(prompt, "• teamniteo/")
				if shown == 0 || shown >= len(repos) || !strings.Contains(prompt, fmt.Sprintf("and %d additional repositories\n\nPermissions:", len(repos)-shown)) {
					t.Fatalf("incorrect overflow summary: %s", prompt)
				}
			}
			for permission, level := range permissions {
				if !strings.Contains(prompt, permission+": "+level) {
					t.Fatalf("permission missing in approval: %s", permission)
				}
			}
			if len(prompt) > 1000 {
				t.Fatal("prompt exceeds native helper limit")
			}
			if missing {
				if err == nil || mintCalls != 0 {
					t.Fatalf("uninstalled repository allowed: %v, %d token calls", err, mintCalls)
				}
				return
			}
			expected := append([]string(nil), repos...)
			sort.Strings(expected)
			if err != nil || mintCalls != 1 || !reflect.DeepEqual(grant.Repositories, expected) || !reflect.DeepEqual(grant.Permissions, permissions) {
				t.Fatalf("incorrect grant: error=%v, calls=%d, repos=%v", err, mintCalls, grant.Repositories)
			}
			if event := audit.events[len(audit.events)-1]; !reflect.DeepEqual(event.Request.Repositories, expected) {
				t.Fatal("audit lost repository scope")
			}
		})
	}
}
