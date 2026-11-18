package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"passlet.dev/passlet/testutil/testkeys"
)

func cacheFixtureGrant() Grant {
	return Grant{ID: "fixture", Token: "fixture-token", User: "alice", ExpiresAt: time.Now().Add(time.Hour), Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}}
}

func cacheFixtureRequest() Request {
	return Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"contents": "read", "metadata": "read"}}
}

func TestCacheScopeAndExpiry(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*Grant)
		want   bool
	}{
		{"exact", func(*Grant) {}, true},
		{"more repositories", func(g *Grant) { g.Repositories = append(g.Repositories, "alice/other") }, true},
		{"write covers read", func(g *Grant) { g.Permissions["contents"] = "write" }, true},
		{"admin covers read", func(g *Grant) { g.Permissions["contents"] = "admin" }, true},
		{"different repository", func(g *Grant) { g.Repositories = []string{"alice/other"} }, false},
		{"different permission", func(g *Grant) { delete(g.Permissions, "contents") }, false},
		{"unknown level", func(g *Grant) { g.Permissions["contents"] = "unknown" }, false},
		{"organization", func(g *Grant) { g.Organization = "alice"; g.Repositories = nil }, true},
		{"account", func(g *Grant) { g.Account = "alice"; g.Repositories = nil }, true},
		{"other organization", func(g *Grant) { g.Organization = "bob"; g.Repositories = nil }, false},
		{"other account", func(g *Grant) { g.Account = "bob"; g.Repositories = nil }, false},
		{"organization missing permission", func(g *Grant) { g.Organization = "alice"; g.Repositories = nil; delete(g.Permissions, "contents") }, false},
		{"near expiry", func(g *Grant) { g.ExpiresAt = time.Now().Add(30 * time.Second) }, false},
		{"enough remaining time", func(g *Grant) { g.ExpiresAt = time.Now().Add(90 * time.Second) }, true},
		{"expired", func(g *Grant) { g.ExpiresAt = time.Now().Add(-time.Second) }, false},
		{"excessive expiry", func(g *Grant) { g.ExpiresAt = time.Now().Add(9 * time.Hour) }, false},
		{"empty token", func(g *Grant) { g.Token = "" }, false},
		{"empty id", func(g *Grant) { g.ID = "" }, false},
		{"empty user", func(g *Grant) { g.User = "" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := cacheFixtureGrant()
			tc.change(&g)
			if grantCovers(g, cacheFixtureRequest()) != tc.want {
				t.Fatal("incorrect scope match")
			}
		})
	}
	r := cacheFixtureRequest()
	r.Permissions["contents"] = "write"
	if grantCovers(cacheFixtureGrant(), r) {
		t.Fatal("read granted write")
	}
	for _, target := range []string{"organization", "account"} {
		r := Request{Permissions: map[string]string{"metadata": "read"}}
		g := cacheFixtureGrant()
		g.Repositories = nil
		if target == "organization" {
			r.Organization = "alice"
			g.Organization = "alice"
		} else {
			r.Account = "alice"
			g.Account = "alice"
		}
		if !grantCovers(g, r) {
			t.Fatal("same target failed")
		}
		if grantCovers(cacheFixtureGrant(), r) {
			t.Fatal("repository grant covered a wider scope")
		}
		other := g
		other.Organization, other.Account = "bob", "bob"
		if grantCovers(other, r) {
			t.Fatal("different target accepted")
		}
		repository := Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"metadata": "read"}}
		if !grantCovers(g, repository) {
			t.Fatal("wide grant did not cover repository metadata")
		}
		narrow := cacheFixtureGrant()
		for _, grants := range [][]Grant{{g, narrow}, {narrow, g}} {
			if best := cachedMatch(grants, repository); best == nil || best.Organization != "" || best.Account != "" {
				t.Fatal("wide grant preferred over repository grant")
			}
		}
	}
}

func TestCacheIdentityIncludesEndpointAndBothKeys(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testkeys.Write(t, os.Getenv("HOME"))
	first, err := grantCacheIdentity("https://one.example")
	if err != nil {
		t.Fatal(err)
	}
	second, err := grantCacheIdentity("https://two.example")
	if err != nil || first == second {
		t.Fatal("endpoints share identity", err)
	}
	testkeys.WriteNamed(t, os.Getenv("HOME"), "other")
	third, err := grantCacheIdentity("https://one.example")
	if err != nil || first == third {
		t.Fatal("pairings share identity", err)
	}
	for _, path := range []string{"connection.json", "agent.pem"} {
		testkeys.Write(t, os.Getenv("HOME"))
		os.Remove(filepath.Join(StateDir(), path))
		if _, err := grantCacheIdentity("https://one.example"); err == nil {
			t.Fatal("missing pairing accepted")
		}
	}
	if _, err := grantCacheIdentity("http://unsafe.example"); err == nil {
		t.Fatal("unsafe endpoint accepted")
	}
}

func saveCacheFixture(t *testing.T, path string, grants []Grant) {
	t.Helper()
	data, err := json.Marshal(grants)
	if err != nil {
		t.Fatal(err)
	}
	if err := writePairingKey(path, data, true); err != nil {
		t.Fatal(err)
	}
}

func TestCacheChoosesNarrowestAndIgnoresExpired(t *testing.T) {
	broad, narrow, expired := cacheFixtureGrant(), cacheFixtureGrant(), cacheFixtureGrant()
	broad.ID = "broad"
	broad.Repositories = append(broad.Repositories, "alice/other")
	expired.ID = "expired"
	expired.ExpiresAt = time.Now().Add(-time.Second)
	grants := []Grant{broad, expired, narrow}
	if got := cachedMatch(grants, cacheFixtureRequest()); got == nil || got.ID != "fixture" {
		t.Fatal("did not choose narrow grant")
	}
	if cachedMatch([]Grant{expired}, cacheFixtureRequest()) != nil {
		t.Fatal("expired grant reused")
	}
}

func TestCacheRejectsUnsafeFiles(t *testing.T) {
	for _, kind := range []string{"shared", "symlink", "hardlink", "directory", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "grant.json")
			switch kind {
			case "shared":
				os.WriteFile(path, []byte("[]"), 0644)
			case "symlink":
				os.Symlink(filepath.Join(dir, "missing"), path)
			case "hardlink":
				os.WriteFile(path, []byte("[]"), 0600)
				os.Link(path, filepath.Join(dir, "other"))
			case "directory":
				os.Mkdir(path, 0700)
			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := readCachedGrants(path); err == nil {
				t.Fatal("unsafe grant file accepted")
			}
		})
	}
}

func TestCacheRejectsMalformedFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	os.Chmod(filepath.Dir(path), 0700)
	if grants, err := readCachedGrants(path); err != nil || len(grants) != 0 {
		t.Fatal("missing cache failed", err)
	}
	for _, body := range []string{"not-json", `[{"unknown":"secret"}]`, "[] {}", strings.Repeat(" ", maxCachedGrants*65536+1)} {
		os.WriteFile(path, []byte(body), 0600)
		if _, err := readCachedGrants(path); err == nil || strings.Contains(err.Error(), "secret") {
			t.Fatal("malformed cache accepted or exposed", err)
		}
	}
	saveCacheFixture(t, path, make([]Grant, maxCachedGrants+1))
	if _, err := readCachedGrants(path); err == nil {
		t.Fatal("oversized cache accepted")
	}
}

func TestCacheLockWaitsAndCancels(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	first, err := lockGrantCache(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := lockGrantCache(ctx, path); err == nil {
		t.Fatal("concurrent lock succeeded")
	}
	done := make(chan error, 1)
	go func() {
		lock, err := lockGrantCache(context.Background(), path)
		if err == nil {
			lock.Close()
		}
		done <- err
	}()
	first.Close()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path + ".lock")
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("lock not private", err)
	}
	os.Chmod(path+".lock", 0644)
	if _, err := lockGrantCache(context.Background(), path); err == nil {
		t.Fatal("shared lock accepted")
	}
}

func TestCachedRequestValidationAndFileSafety(t *testing.T) {
	for _, kind := range []string{"request", "pairing", "state directory", "cache directory", "cache symlink", "corrupt cache", "lock symlink", "unavailable approver"} {
		t.Run(kind, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			testkeys.Write(t, os.Getenv("HOME"))
			endpoint := "unix:///unavailable"
			path, err := grantCachePath(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			request := cacheFixtureRequest()
			switch kind {
			case "request":
				request = Request{}
			case "pairing":
				os.Remove(filepath.Join(StateDir(), "connection.json"))
			case "state directory":
				os.Chmod(StateDir(), 0755)
			case "cache directory":
				os.Mkdir(GrantCacheDir(), 0755)
			case "cache symlink":
				os.Symlink(t.TempDir(), GrantCacheDir())
			case "corrupt cache":
				os.Mkdir(GrantCacheDir(), 0700)
				os.WriteFile(path, []byte("bad"), 0600)
			case "lock symlink":
				os.Mkdir(GrantCacheDir(), 0700)
				os.Symlink(filepath.Join(t.TempDir(), "missing"), path+".lock")
			}
			if _, err := RequestCachedGrant(context.Background(), endpoint, request); err == nil {
				t.Fatal("invalid request/cache accepted")
			}
		})
	}
}

func TestCachedHitWorksOfflineWhileLockHeld(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testkeys.Write(t, os.Getenv("HOME"))
	endpoint := "unix:///unavailable"
	path, err := grantCachePath(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	approved := cacheFixtureGrant()
	saveCacheFixture(t, path, []Grant{approved})
	lock, err := lockGrantCache(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	grant, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest())
	if err != nil || grant.ID != approved.ID || !grant.ExpiresAt.Equal(approved.ExpiresAt) {
		t.Fatal("offline reuse failed", err)
	}
	other, err := grantCachePath("unix:///other")
	if err != nil || other == path {
		t.Fatal("endpoints share cache", err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("cache file not private", err)
	}
	info, err = os.Stat(GrantCacheDir())
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatal("cache directory not private", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	request := cacheFixtureRequest()
	request.Permissions["contents"] = "write"
	if _, err := RequestCachedGrant(ctx, endpoint, request); err == nil {
		t.Fatal("waiting request ignored cancellation")
	}
}

func TestCacheReadsDuringAtomicReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "grants.json")
	os.Chmod(filepath.Dir(path), 0700)
	first := cacheFixtureGrant()
	second := cacheFixtureGrant()
	second.ID = "replacement"
	saveCacheFixture(t, path, []Grant{first})
	data, _ := json.Marshal([]Grant{second})
	done := make(chan error, 1)
	go func() {
		for range 50 {
			if err := writePairingKey(path, data, true); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	defer func() {
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	for range 100 {
		grants, err := readCachedGrants(path)
		if err != nil || len(grants) != 1 {
			t.Fatal("partial or unreadable cache", err)
		}
		if grants[0].ID != first.ID && grants[0].ID != second.ID {
			t.Fatal("unexpected grant")
		}
	}
}

func TestDiscardOnlyRejectedTokens(t *testing.T) {
	for _, status := range []int{200, 401, 403, 404, 429, 500, 302, 0} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			testkeys.Write(t, os.Getenv("HOME"))
			endpoint := "unix:///unavailable"
			path, err := grantCachePath(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			rejected := cacheFixtureGrant()
			replacement := rejected
			replacement.Token = "replacement"
			saveCacheFixture(t, path, []Grant{rejected, rejected, replacement})
			previous := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = previous })
			calls := 0
			http.DefaultTransport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.String() != "https://api.github.com/user" || r.Header.Get("Authorization") != "Bearer "+rejected.Token {
					t.Fatal("wrong token validation request")
				}
				if status == 0 {
					return nil, errors.New("offline")
				}
				return &http.Response{StatusCode: status, Header: http.Header{"Location": {"https://elsewhere.example"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
			})
			if err := DiscardRejectedGrant(context.Background(), endpoint, rejected); err != nil {
				t.Fatal(err)
			}
			grants, err := readCachedGrants(path)
			want := 3
			if status == 401 {
				want = 1
			}
			if err != nil || len(grants) != want || grants[len(grants)-1].Token != replacement.Token || calls != 1 {
				t.Fatal("incorrect eviction or redirect followed", err)
			}
			if err := discardCachedGrant(context.Background(), endpoint, Grant{Token: "already-replaced"}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDiscardCacheFailures(t *testing.T) {
	for _, mode := range []string{"pairing", "directory", "lock", "corrupt", "write", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("HOME", t.TempDir())
			testkeys.Write(t, os.Getenv("HOME"))
			endpoint := "unix:///unavailable"
			path, err := grantCachePath(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			grant := cacheFixtureGrant()
			saveCacheFixture(t, path, []Grant{grant})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "pairing":
				os.Remove(filepath.Join(StateDir(), "connection.json"))
			case "directory":
				os.Chmod(GrantCacheDir(), 0755)
			case "lock":
				os.WriteFile(path+".lock", nil, 0644)
			case "corrupt":
				os.WriteFile(path, []byte("bad"), 0600)
			case "write":
				lock, err := lockGrantCache(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				lock.Close()
				os.Chmod(GrantCacheDir(), 0500)
				defer os.Chmod(GrantCacheDir(), 0700)
			case "cancelled":
				lock, err := lockGrantCache(ctx, path)
				if err != nil {
					t.Fatal(err)
				}
				defer lock.Close()
				cancel()
			}
			if err := discardCachedGrant(ctx, endpoint, grant); err == nil {
				t.Fatal("cache failure ignored")
			}
		})
	}
}

func TestRejectedGrantEvictionWaitsForPendingApproval(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	testkeys.Write(t, os.Getenv("HOME"))
	synctest.Test(t, func(t *testing.T) {
		endpoint := "unix:///unavailable"
		path, err := grantCachePath(endpoint)
		if err != nil {
			t.Fatal(err)
		}
		rejected := cacheFixtureGrant()
		saveCacheFixture(t, path, []Grant{rejected})
		lock, err := lockGrantCache(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		done := make(chan error, 1)
		go func() { done <- discardCachedGrant(context.Background(), endpoint, rejected) }()
		time.Sleep(ConnectDeadline + time.Second)
		select {
		case err := <-done:
			t.Fatal("eviction stopped before approval completed", err)
		default:
		}
		approved := cacheFixtureGrant()
		approved.Token = "newly-approved"
		saveCacheFixture(t, path, []Grant{rejected, approved})
		lock.Close()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		grants, err := readCachedGrants(path)
		if err != nil || len(grants) != 1 || grants[0].Token != approved.Token {
			t.Fatal("rejected token retained or new approval lost", err)
		}
	})
}
