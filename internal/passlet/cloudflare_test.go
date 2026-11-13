package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const cfZone = "11111111111111111111111111111111"
const cfAccount = "22222222222222222222222222222222"
const cfToken = "33333333333333333333333333333333"
const cfDns = "44444444444444444444444444444444"
const cfWorkers = "55555555555555555555555555555555"
const cfAdmin = "66666666666666666666666666666666"

func cfRequest() Request {
	return Request{Provider: "cloudflare", Zones: []string{cfZone}, Permissions: map[string]string{"dns": "write"}}
}
func cfCredentials() Credentials {
	return Credentials{Provider: "cloudflare", AccessToken: "parent-secret", CloudflareTokenID: cfToken}
}
func cfGroups() []cloudflareGroup {
	return []cloudflareGroup{
		{ID: cfDns, Name: "DNS Write", Scopes: []string{"com.cloudflare.api.account.zone"}},
		{ID: cfWorkers, Name: "Workers Scripts Edit", Scopes: []string{"com.cloudflare.api.account"}},
		{ID: cfAdmin, Name: "API Tokens Write", Scopes: []string{"com.cloudflare.api.user"}},
	}
}
func cfJSON(w http.ResponseWriter, result any) {
	_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "result": result})
}
func cfAPI(t *testing.T, handler http.HandlerFunc) *Cloudflare {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	c := NewCloudflare()
	c.API = server.URL
	return c
}

func TestCloudflareValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Request)
	}{
		{"missing resources", func(r *Request) { r.Zones = nil }},
		{"mixed scope", func(r *Request) { r.Account = cfAccount }},
		{"github organization", func(r *Request) { r.Organization = "acme" }},
		{"github repos", func(r *Request) { r.Repositories = []string{"acme/repo"} }},
		{"wildcard", func(r *Request) { r.Zones = []string{"*"} }},
		{"duplicate zone", func(r *Request) { r.Zones = []string{cfZone, cfZone} }},
		{"too many zones", func(r *Request) { r.Zones = []string{cfZone, cfAccount, cfToken, cfDns} }},
		{"invalid account", func(r *Request) { r.Zones = nil; r.Account = "all" }},
		{"missing permissions", func(r *Request) { r.Permissions = nil }},
		{"too many permissions", func(r *Request) {
			r.Permissions = map[string]string{"a": "read", "b": "read", "c": "read", "d": "read", "e": "read"}
		}},
		{"injected permission", func(r *Request) { r.Permissions = map[string]string{"dns\nread": "write"} }},
		{"unsupported level", func(r *Request) { r.Permissions["dns"] = "admin" }},
		{"token delegation", func(r *Request) { r.Permissions = map[string]string{"account_api_tokens": "write"} }},
		{"unknown provider", func(r *Request) { r.Provider = "other" }},
		{"github zones", func(r *Request) { r.Provider = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := cfRequest()
			tc.change(&r)
			if r.Validate() == nil {
				t.Fatal("accepted invalid request")
			}
		})
	}
	r := cfRequest()
	r.Zones = []string{cfAccount, cfZone}
	r.Agent = "Codex"
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	prompt := r.Prompt(Caller{Name: "Mac\ninjection", Transport: "unix"})
	for _, part := range []string{"Cloudflare", "Codex from local", "1 hour", cfZone, cfAccount, "dns: write"} {
		if !strings.Contains(prompt, part) {
			t.Fatal(prompt)
		}
	}
	r.Zones = nil
	r.Account = cfAccount
	r.Agent = ""
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
	if prompt = r.Prompt(Caller{Name: "remote"}); !strings.Contains(prompt, "Shell from remote") || !strings.Contains(prompt, "Account: "+cfAccount) {
		t.Fatal(prompt)
	}
	c := cfCredentials()
	if c.Validate() != nil {
		t.Fatal("valid credential rejected")
	}
	for _, bad := range []Credentials{{Provider: "other"}, {Provider: "cloudflare"}, {Provider: "cloudflare", AccessToken: "secret", Account: AccountInfo{Login: "bad"}}} {
		if bad.Validate() == nil {
			t.Fatal("invalid credential accepted")
		}
	}
	for name, want := range map[string]string{"DNS Write": "dns:write", "Workers Scripts Edit": "workers_scripts:write", "Access: Apps and Policies Revoke": "access_apps_and_policies:revoke", "invalid": ":"} {
		base, level := cloudflarePermissionKey(name)
		if base+":"+level != want {
			t.Fatal(name, base, level)
		}
	}
}

func TestCloudflareMint(t *testing.T) {
	for _, mode := range []string{"zone", "account", "groups failure", "mint failure", "ambiguous", "wrong resource scope", "unknown permission", "extra resource", "extra permission", "wrong permission", "duplicate permission", "deny policy", "extra policy", "no expiry", "long expiry", "expired", "no token", "invalid id", "inactive"} {
		t.Run(mode, func(t *testing.T) {
			request := cfRequest()
			if mode == "account" {
				request.Zones = nil
				request.Account = cfAccount
				request.Permissions = map[string]string{"workers_scripts": "write"}
			}
			posts := 0
			c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer parent-secret" {
					t.Error("wrong credential")
				}
				if r.URL.Path == "/user/tokens/permission_groups" {
					if mode == "groups failure" {
						w.WriteHeader(403)
						return
					}
					groups := cfGroups()
					switch mode {
					case "ambiguous":
						groups = append(groups, cloudflareGroup{ID: cfWorkers, Name: "DNS Edit", Scopes: []string{"com.cloudflare.api.account.zone"}})
					case "wrong resource scope":
						groups[0].Scopes = []string{"com.cloudflare.api.account"}
					case "unknown permission":
						groups[0].Name = "Other Read"
					}
					cfJSON(w, groups)
					return
				}
				if r.URL.Path != "/user/tokens" || r.Method != "POST" {
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					w.WriteHeader(404)
					return
				}
				posts++
				if mode == "mint failure" {
					w.WriteHeader(403)
					return
				}
				var body struct {
					Policies  []cloudflarePolicy `json:"policies"`
					ExpiresAt time.Time          `json:"expires_on"`
				}
				if json.NewDecoder(r.Body).Decode(&body) != nil {
					t.Fatal("invalid request")
				}
				if time.Until(body.ExpiresAt) > time.Hour || time.Until(body.ExpiresAt) < 59*time.Minute {
					t.Error("wrong expiry")
				}
				key := "com.cloudflare.api.account.zone." + cfZone
				permission := cfDns
				if mode == "account" {
					key = "com.cloudflare.api.account." + cfAccount
					permission = cfWorkers
				}
				if len(body.Policies) != 1 || body.Policies[0].Resources[key] != "*" || body.Policies[0].Groups[0].ID != permission {
					t.Errorf("wrong policy %+v", body.Policies)
				}
				issued := cloudflareToken{ID: cfToken, Value: "child-secret", Status: "active", ExpiresAt: body.ExpiresAt, Policies: body.Policies}
				switch mode {
				case "extra resource":
					issued.Policies[0].Resources["com.cloudflare.api.account.*"] = "*"
				case "extra permission":
					issued.Policies[0].Groups = append(issued.Policies[0].Groups, cloudflareGroup{ID: cfWorkers})
				case "wrong permission":
					issued.Policies[0].Groups[0].ID = cfWorkers
				case "duplicate permission":
					issued.Policies[0].Groups = append(issued.Policies[0].Groups, issued.Policies[0].Groups[0])
				case "deny policy":
					issued.Policies[0].Effect = "deny"
				case "extra policy":
					issued.Policies = append(issued.Policies, issued.Policies[0])
				case "no expiry":
					issued.ExpiresAt = time.Time{}
				case "long expiry":
					issued.ExpiresAt = issued.ExpiresAt.Add(time.Second)
				case "expired":
					issued.ExpiresAt = time.Now().Add(-time.Hour)
				case "no token":
					issued.Value = ""
				case "invalid id":
					issued.ID = "bad"
				case "inactive":
					issued.Status = "disabled"
				}
				cfJSON(w, issued)
			})
			grant, err := c.Mint(context.Background(), cfCredentials(), request)
			if mode == "zone" || mode == "account" {
				if err != nil || grant.Token != "child-secret" || grant.Provider != "cloudflare" || grant.Account != request.Account || grant.CloudflareTokenID != cfToken {
					t.Fatalf("mint failed: %v", err)
				}
			} else if err == nil || grant.Token != "" {
				t.Fatal("delivered invalid token")
			}
			if mode == "ambiguous" || mode == "wrong resource scope" || mode == "unknown permission" || mode == "groups failure" {
				if posts != 0 {
					t.Fatal("minted before scope resolution")
				}
			}
		})
	}
	c := NewCloudflare()
	if _, err := c.Mint(context.Background(), cfCredentials(), Request{}); err == nil {
		t.Fatal("invalid request")
	}
	if _, err := c.Mint(context.Background(), Credentials{}, cfRequest()); err == nil {
		t.Fatal("invalid credentials")
	}
}

func TestCloudflareVerify(t *testing.T) {
	for _, mode := range []string{"valid", "edit", "verify failure", "inactive", "expired", "details failure", "groups failure", "missing permission", "deny"} {
		t.Run(mode, func(t *testing.T) {
			c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Error("setup must not mint tokens")
				}
				switch r.URL.Path {
				case "/user/tokens/verify":
					if mode == "verify failure" {
						w.WriteHeader(401)
						return
					}
					v := cloudflareToken{ID: cfToken, Status: "active"}
					if mode == "inactive" {
						v.Status = "disabled"
					}
					if mode == "expired" {
						v.ExpiresAt = time.Now().Add(-time.Hour)
					}
					cfJSON(w, v)
				case "/user/tokens/" + cfToken:
					if mode == "details failure" {
						w.WriteHeader(403)
						return
					}
					policy := cloudflarePolicy{Effect: "allow", Groups: []cloudflareGroup{{ID: cfAdmin}}}
					if mode == "deny" {
						policy.Effect = "deny"
					}
					if mode == "missing permission" {
						policy.Groups[0].ID = cfDns
					}
					cfJSON(w, cloudflareToken{Policies: []cloudflarePolicy{policy}})
				case "/user/tokens/permission_groups":
					if mode == "groups failure" {
						w.WriteHeader(403)
						return
					}
					groups := cfGroups()
					if mode == "edit" {
						groups[2].Name = "API Tokens Edit"
					}
					cfJSON(w, groups)
				default:
					t.Error(r.URL.Path)
				}
			})
			credentials, err := c.Verify(context.Background(), "parent-secret")
			if mode == "valid" || mode == "edit" {
				if err != nil || credentials.Validate() != nil || credentials.AccessToken != "parent-secret" {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("accepted invalid credential")
			}
		})
	}
}

type cfTransport func(*http.Request) (*http.Response, error)

func (f cfTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type cfBrokenBody struct{}

func (cfBrokenBody) Read([]byte) (int, error) { return 0, errors.New("parent-secret") }
func (cfBrokenBody) Close() error             { return nil }

func TestCloudflareTransportErrors(t *testing.T) {
	c := NewCloudflare()
	var result any
	if err := c.call(context.Background(), "secret", "POST", "/", make(chan int), &result); err == nil {
		t.Fatal("invalid body")
	}
	c.API = "://"
	if err := c.call(context.Background(), "secret", "GET", "/", nil, &result); err == nil {
		t.Fatal("invalid URL")
	}
	for _, body := range []string{"not JSON", `{"success":false,"result":{}}`, `{"success":true,"result":null`, strings.Repeat("x", 1048577)} {
		c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })
		if err := c.call(context.Background(), "secret", "GET", "/", nil, &result); err == nil {
			t.Fatal("invalid response")
		}
	}
	for _, broken := range []bool{false, true} {
		c := NewCloudflare()
		c.HTTP.Transport = cfTransport(func(*http.Request) (*http.Response, error) {
			if !broken {
				return nil, errors.New("parent-secret")
			}
			return &http.Response{StatusCode: 200, Body: cfBrokenBody{}}, nil
		})
		if err := c.call(context.Background(), "secret", "GET", "/", nil, &result); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("unsanitized failure", err)
		}
	}
	called := false
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true }))
	defer target.Close()
	c = cfAPI(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, http.StatusFound) })
	if err := c.call(context.Background(), "secret", "GET", "/", nil, &result); err == nil || called {
		t.Fatal("followed redirect")
	}
}

func TestCloudflareServerAndClient(t *testing.T) {
	for _, mode := range []string{"success", "denied", "unconfigured", "wrong credentials", "mint failure"} {
		t.Run(mode, func(t *testing.T) {
			audit := &memoryAudit{}
			github := &testMinter{}
			githubApproval := &testApprover{}
			cfApproval := &testApprover{session: &testSession{c: cfCredentials()}}
			if mode == "denied" {
				cfApproval.err = errors.New("approval_denied")
			}
			if mode == "wrong credentials" {
				cfApproval.session.c = Credentials{}
			}
			c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "mint failure" {
					w.WriteHeader(403)
					return
				}
				if r.URL.Path == "/user/tokens/permission_groups" {
					cfJSON(w, cfGroups())
					return
				}
				var token cloudflareToken
				if json.NewDecoder(r.Body).Decode(&token) != nil {
					t.Error("decode")
				}
				token.ID = cfToken
				token.Value = "child-secret"
				token.Status = "active"
				cfJSON(w, token)
			})
			server := testServer(githubApproval, github, audit)
			server.Cloudflare = c
			server.CloudflareApproval = cfApproval
			if mode == "unconfigured" {
				server.Cloudflare = nil
			}
			httpServer := httptest.NewServer(server)
			defer httpServer.Close()
			// requestGrant validates HTTPS origins, so route its request into this fixture transport.
			client := httpServer.Client()
			client.Transport = cfTransport(func(r *http.Request) (*http.Response, error) {
				r.URL.Scheme = "http"
				r.URL.Host = strings.TrimPrefix(httpServer.URL, "http://")
				return http.DefaultTransport.RoundTrip(r)
			})
			grant, err := requestGrant(context.Background(), "https://fixture", cfRequest(), client)
			if mode == "success" {
				if err != nil || grant.Token != "child-secret" || !cfApproval.session.closed {
					t.Fatal(err)
				}
				event := audit.events[len(audit.events)-1]
				if event.CloudflareTokenID != cfToken || event.GitHubUser != "" || len(event.GrantedZones) != 1 {
					t.Fatal("incorrect audit")
				}
			} else if err == nil {
				t.Fatal("accepted failed request")
			}
			if github.calls != 0 || len(githubApproval.prompts) != 0 {
				t.Fatal("Cloudflare request accessed GitHub")
			}
			for _, event := range audit.events {
				data, _ := json.Marshal(event)
				if strings.Contains(string(data), "secret") {
					t.Fatal("audit leaked secret")
				}
			}
		})
	}
}

func TestCloudflareHelperIsolation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "helper")
	c := cfCredentials()
	data, _ := json.Marshal(helperReply{OK: true, Credentials: &c})
	script := "#!/bin/sh\n[ \"$1\" = --provider ] && [ \"$2\" = cloudflare ] || exit 2\nread request\nprintf '%s\\n' '" + string(data) + "'\nread request\nprintf '%s\\n' '{\"ok\":true}'\ncat >/dev/null\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	h := NativeHelper{Path: path, Provider: "cloudflare"}
	if _, err := h.Reset(context.Background()); err != nil {
		t.Fatal(err)
	}
	script = "#!/bin/sh\n[ \"$1\" = --import ] && [ \"$2\" = --provider ] && [ \"$3\" = cloudflare ] || exit 2\ncat >/dev/null\nprintf '%s\\n' '{\"ok\":true}'\n"
	if err := os.WriteFile(path, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	if err := ImportCredentials(context.Background(), path, c); err != nil {
		t.Fatal(err)
	}
}

func TestCloudflareClientRejectsUnexpectedScope(t *testing.T) {
	for _, mode := range []string{"provider", "zone", "account", "permission", "expiry", "token id"} {
		t.Run(mode, func(t *testing.T) {
			request := cfRequest()
			grant := Grant{Provider: "cloudflare", CloudflareTokenID: cfToken, ID: "grant", Token: "child-secret", Zones: []string{cfZone}, Permissions: map[string]string{"dns": "write"}, ExpiresAt: time.Now().Add(time.Hour)}
			switch mode {
			case "provider":
				grant.Provider = ""
				grant.User = "alice"
			case "zone":
				grant.Zones = []string{cfAccount}
			case "account":
				grant.Account = cfAccount
			case "permission":
				grant.Permissions["dns"] = "read"
			case "expiry":
				grant.ExpiresAt = time.Now().Add(2 * time.Hour)
			case "token id":
				grant.CloudflareTokenID = ""
			}
			client := &http.Client{Transport: cfTransport(func(*http.Request) (*http.Response, error) {
				data, _ := json.Marshal(grant)
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(data)))}, nil
			})}
			if _, err := requestGrant(context.Background(), "https://fixture", request, client); err == nil {
				t.Fatal("accepted mismatched grant")
			}
		})
	}
}

func TestCloudflareDoctor(t *testing.T) {
	for _, mode := range []string{"valid", "denied", "missing", "unconfigured", "wrong provider", "revoked", "no authority", "wrong token"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			c := cfAPI(t, func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "GET" || r.ContentLength != 0 || r.Header.Get("Authorization") != "Bearer parent-secret" {
					t.Error("diagnostics must only read provider state with the parent credential")
				}
				if mode == "revoked" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				switch r.URL.Path {
				case "/user/tokens/verify":
					id := cfToken
					if mode == "wrong token" {
						id = cfZone
					}
					cfJSON(w, cloudflareToken{ID: id, Status: "active"})
				case "/user/tokens/" + cfToken, "/user/tokens/" + cfZone:
					groups := []cloudflareGroup{{ID: cfAdmin}}
					if mode == "no authority" {
						groups = nil
					}
					cfJSON(w, cloudflareToken{Policies: []cloudflarePolicy{{Effect: "allow", Groups: groups}}})
				case "/user/tokens/permission_groups":
					cfJSON(w, cfGroups())
				default:
					t.Error("unexpected Cloudflare call", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			})
			approval := &testApprover{session: &testSession{c: cfCredentials()}}
			githubApproval := &testApprover{}
			github := &testMinter{}
			audit := &memoryAudit{}
			server := testServer(githubApproval, github, audit)
			server.Cloudflare = c
			server.CloudflareApproval = approval
			want := ""
			switch mode {
			case "denied":
				approval.err = errors.New("approval_denied")
				want = "approval_denied"
			case "missing":
				approval.err = errors.New("credentials_missing")
				want = "credentials_missing"
			case "unconfigured":
				server.Cloudflare = nil
				want = "provider_unavailable"
			case "wrong provider":
				approval.session.c = Credentials{}
				want = "invalid_credentials"
			case "revoked", "no authority", "wrong token":
				want = "cloudflare_credentials_verification_failed"
			}
			path := socketPath(t)
			listener, err := ListenUnix(path)
			if err != nil {
				t.Fatal(err)
			}
			httpServer := &http.Server{Handler: server}
			listener = pairedFixture(t, httpServer, listener)
			go func() { _ = httpServer.Serve(listener) }()
			defer httpServer.Close()
			result, err := Doctor(context.Background(), "unix://"+path, "cloudflare")
			if want != "" {
				if err == nil || err.Error() != want {
					t.Fatalf("want %s, got %v", want, err)
				}
			} else {
				if err != nil || result.Provider != "cloudflare" || result.CloudflareTokenID != cfToken || result.Login != "" {
					t.Fatal(result, err)
				}
				if !approval.session.closed || approval.session.stores != 0 || calls != 3 {
					t.Fatal("incorrect diagnostic lifecycle")
				}
				if len(approval.prompts) != 1 || !strings.Contains(approval.prompts[0], "Cloudflare") {
					t.Fatal("provider not shown in approval")
				}
				verified := false
				for _, event := range audit.events {
					if event.Operation != "doctor" || event.Request.Provider != "cloudflare" || event.GitHubUser != "" {
						t.Fatal("incorrect provider audit")
					}
					if event.Outcome == "verified" {
						verified = true
						if event.CloudflareTokenID != cfToken {
							t.Fatal("missing token identity")
						}
					}
					data, _ := json.Marshal(event)
					if strings.Contains(string(data), "secret") {
						t.Fatal("audit leaked secret")
					}
				}
				if !verified {
					t.Fatal("missing verification audit")
				}
			}
			if github.calls != 0 || len(githubApproval.prompts) != 0 {
				t.Fatal("Cloudflare diagnostics accessed GitHub")
			}
			if (mode == "denied" || mode == "missing" || mode == "unconfigured" || mode == "wrong provider") && calls != 0 {
				t.Fatal("provider called before approval or credential selection")
			}
		})
	}
}

func TestCloudflareDoctorRejectsInvalidMessages(t *testing.T) {
	if _, err := Doctor(context.Background(), "unix:///missing", "unknown"); err == nil || err.Error() != "invalid_provider" {
		t.Fatal(err)
	}
	server := testServer(&testApprover{}, &testMinter{}, &memoryAudit{})
	for _, body := range []string{`{"provider":"unknown"}`, `{"provider":"cloudflare","zones":["` + cfZone + `"]}`, `{"provider":"cloudflare","permissions":{"dns":"read"}}`} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, httptest.NewRequest("POST", "/v1/doctor", strings.NewReader(body)))
		if response.Code != http.StatusBadRequest {
			t.Fatal("doctor accepted grant scope or invalid provider")
		}
	}
	for _, body := range []string{`{"login":"alice"}`, `{"provider":"cloudflare","cloudflare_token_id":"bad"}`, `{"provider":"cloudflare","cloudflare_token_id":"` + cfToken + `","login":"alice"}`} {
		path := socketPath(t)
		listener, err := ListenUnix(path)
		if err != nil {
			t.Fatal(err)
		}
		server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, body) })}
		listener = pairedFixture(t, server, listener)
		go func() { _ = server.Serve(listener) }()
		_, err = Doctor(context.Background(), "unix://"+path, "cloudflare")
		server.Close()
		if err == nil || err.Error() != "invalid_doctor_response" {
			t.Fatal("accepted wrong provider response", err)
		}
	}
}
