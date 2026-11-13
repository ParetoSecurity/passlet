package passlet

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCloudflareResourceDiscovery(t *testing.T) {
	for _, mode := range []string{"success", "groups failure", "missing permission", "ambiguous permission", "create failure", "invalid token", "bad policy", "expired", "list failure", "invalid resource", "empty name", "duplicate", "revoke failure", "cancelled", "zones failure", "write failure"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			created, revoked, listed := 0, 0, 0
			c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/accounts" && r.URL.Path != "/zones" && r.Header.Get("Authorization") != "Bearer parent-secret" {
					t.Error("parent authentication missing")
				}
				switch {
				case r.URL.Path == "/user/tokens/permission_groups":
					if mode == "groups failure" {
						w.WriteHeader(403)
						return
					}
					groups := []cloudflareGroup{
						{ID: cfWorkers, Name: "Account Settings Read", Scopes: []string{"com.cloudflare.api.account"}},
						{ID: cfDns, Name: "Zone Read", Scopes: []string{"com.cloudflare.api.account.zone"}},
					}
					if mode == "missing permission" {
						groups = nil
					}
					if mode == "ambiguous permission" {
						groups = append(groups, groups[0])
					}
					cfJSON(w, groups)
				case r.Method == "POST" && r.URL.Path == "/user/tokens":
					if mode == "create failure" {
						w.WriteHeader(403)
						return
					}
					var body struct {
						Name      string             `json:"name"`
						Policies  []cloudflarePolicy `json:"policies"`
						ExpiresAt time.Time          `json:"expires_on"`
					}
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					if body.Name != "Passlet resource discovery" || time.Until(body.ExpiresAt) > 5*time.Minute || time.Until(body.ExpiresAt) < 4*time.Minute {
						t.Error("unexpected discovery lifetime or name")
					}
					scope, group := "com.cloudflare.api.account.*", cfWorkers
					if created == 1 {
						scope, group = "com.cloudflare.api.account.zone.*", cfDns
					}
					if !sameCloudflarePolicies(body.Policies, []cloudflarePolicy{{Effect: "allow", Resources: map[string]string{scope: "*"}, Groups: []cloudflareGroup{{ID: group}}}}) {
						t.Error("unexpected discovery permissions")
					}
					token := cloudflareToken{ID: cfAdmin, Value: "discovery-secret", Status: "active", Policies: body.Policies, ExpiresAt: body.ExpiresAt}
					if mode == "invalid token" {
						token.ID = "invalid"
					}
					if mode == "bad policy" {
						token.Policies = nil
					}
					if mode == "expired" {
						token.ExpiresAt = time.Now().Add(-time.Minute)
					}
					created++
					cfJSON(w, token)
				case r.Method == "DELETE":
					if r.URL.Path != "/user/tokens/"+cfAdmin {
						t.Error("wrong token revoked")
					}
					revoked++
					if mode == "revoke failure" {
						w.WriteHeader(503)
						return
					}
					cfJSON(w, map[string]string{"id": cfAdmin})
				case r.URL.Path == "/accounts" || r.URL.Path == "/zones":
					listed++
					if r.Header.Get("Authorization") != "Bearer discovery-secret" || r.URL.Query().Get("per_page") != "50" {
						t.Error("incorrect discovery request")
					}
					if mode == "list failure" || mode == "zones failure" && r.URL.Path == "/zones" {
						w.WriteHeader(403)
						return
					}
					if mode == "cancelled" {
						cancel()
						<-r.Context().Done()
						return
					}
					items := []cloudflareResource{}
					if r.URL.Path == "/zones" {
						items = append(items, cloudflareResource{cfZone, "example.com"})
					} else if r.URL.Query().Get("page") == "1" || mode == "duplicate" {
						for i := range 50 {
							items = append(items, cloudflareResource{fmt.Sprintf("%032x", i), fmt.Sprintf("Account %d", i)})
						}
					}
					if mode == "invalid resource" {
						items[0].ID = "invalid"
					}
					if mode == "empty name" {
						items[0].Name = ""
					}
					cfJSON(w, items)
				default:
					t.Error(r.Method, r.URL.Path)
					w.WriteHeader(404)
				}
			})
			if mode == "write failure" {
				os.MkdirAll(filepath.Dir(StateDir()), 0700)
				os.WriteFile(StateDir(), nil, 0600)
			}
			err := c.RefreshResources(ctx, cfCredentials())
			if (err == nil) != (mode == "success") {
				t.Fatalf("unexpected outcome: %v", err)
			}
			wantRevoked := created
			if mode == "invalid token" {
				wantRevoked = 0
			}
			if revoked != wantRevoked {
				t.Fatalf("created %d, revoked %d", created, revoked)
			}
			if mode == "success" {
				catalog := loadCloudflareCatalog()
				if len(catalog.Accounts) != 50 || catalog.Zones[cfZone] != "example.com" || listed != 3 {
					t.Fatal(catalog, listed)
				}
				info, _ := os.Stat(cloudflareCatalogPath())
				data, _ := os.ReadFile(cloudflareCatalogPath())
				if info.Mode().Perm() != 0600 || strings.Contains(string(data), "secret") {
					t.Fatal("unsafe catalog")
				}
				prompt := cfRequest().Prompt(Caller{Transport: "unix"})
				if !strings.Contains(prompt, cfZone+" (example.com, cached)") {
					t.Fatal(prompt)
				}
				accountRequest := cfRequest()
				accountRequest.Zones, accountRequest.Account = nil, strings.Repeat("0", 32)
				if !strings.Contains(accountRequest.Prompt(Caller{}), "Account 0, cached") {
					t.Fatal("account label missing")
				}
			} else if _, err := os.Stat(cloudflareCatalogPath()); err == nil {
				t.Fatal("failed discovery published catalog")
			}
		})
	}
}

func TestCloudflareResourceCatalogFallback(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	c := NewCloudflare()
	if err := c.RefreshResources(context.Background(), Credentials{}); err == nil {
		t.Fatal("accepted invalid credentials")
	}
	if label := cloudflareResourceLabel(cfZone, nil); label != cfZone {
		t.Fatal(label)
	}
	if label := cloudflareResourceLabel(cfZone, map[string]string{cfZone: "name\n\x1b\u202e"}); strings.ContainsAny(label, "\n\x1b\u202e") || !strings.HasPrefix(label, cfZone) {
		t.Fatal(label)
	}
	if err := PrivateDir(StateDir()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cloudflareCatalogPath(), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if c := loadCloudflareCatalog(); c.Accounts != nil || c.Zones != nil {
		t.Fatal("invalid catalog accepted")
	}
}
