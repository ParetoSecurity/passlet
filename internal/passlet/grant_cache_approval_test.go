//go:build !agentonly

package passlet

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"passlet.dev/passlet/testutil/testkeys"
)

func cacheApprover(t *testing.T, handler http.HandlerFunc) (string, *http.Server) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	dir, err := os.MkdirTemp("/tmp", "pl-cache-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	socket := filepath.Join(dir, "approver.sock")
	listener, err := ListenUnix(socket)
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: handler}
	listener = pairedFixture(t, server, listener)
	go func() { server.Serve(listener) }()
	t.Cleanup(func() { server.Close() })
	return "unix://" + socket, server
}

func TestCachedGrantApprovalAndStorageFailures(t *testing.T) {
	for _, mode := range []string{"reused offline", "changed pairing", "removed pairing", "unwritable cache"} {
		t.Run(mode, func(t *testing.T) {
			endpoint, server := cacheApprover(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "changed pairing" {
					testkeys.WriteNamed(t, os.Getenv("HOME"), "new")
				}
				if mode == "removed pairing" {
					os.Remove(connectionPath())
				}
				if mode == "unwritable cache" {
					os.Chmod(GrantCacheDir(), 0500)
				}
				json.NewEncoder(w).Encode(cacheFixtureGrant())
			})
			defer os.Chmod(GrantCacheDir(), 0700)
			grant, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest())
			if mode != "reused offline" {
				if err == nil {
					t.Fatal("failure ignored")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			server.Close()
			reused, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest())
			if err != nil || reused.Token != grant.Token || !reused.ExpiresAt.Equal(grant.ExpiresAt) {
				t.Fatal("offline reuse failed", err)
			}
		})
	}
}

func TestConcurrentCachedRequestsApproveOnce(t *testing.T) {
	var approvals atomic.Int32
	endpoint, _ := cacheApprover(t, func(w http.ResponseWriter, r *http.Request) {
		approvals.Add(1)
		time.Sleep(50 * time.Millisecond)
		json.NewEncoder(w).Encode(cacheFixtureGrant())
	})
	var workers sync.WaitGroup
	for range 8 {
		workers.Go(func() {
			grant, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest())
			if err != nil || grant.ID != "fixture" {
				t.Error("request failed", err)
			}
		})
	}
	workers.Wait()
	if approvals.Load() != 1 {
		t.Fatal("duplicate approval", approvals.Load())
	}
}

func TestCachePrunesAndBoundsOnWrite(t *testing.T) {
	for _, expired := range []bool{true, false} {
		t.Run(map[bool]string{true: "expired", false: "full"}[expired], func(t *testing.T) {
			endpoint, _ := cacheApprover(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(cacheFixtureGrant()) })
			path, err := grantCachePath(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			grants := make([]Grant, maxCachedGrants)
			for i := range grants {
				grants[i] = cacheFixtureGrant()
				grants[i].Repositories = []string{"alice/other"}
				if expired {
					grants[i].ExpiresAt = time.Now().Add(-time.Hour)
				}
			}
			saveCacheFixture(t, path, grants)
			if _, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest()); err != nil {
				t.Fatal(err)
			}
			stored, err := readCachedGrants(path)
			if err != nil {
				t.Fatal(err)
			}
			want := maxCachedGrants
			if expired {
				want = 1
			}
			if len(stored) != want || cachedMatch(stored, cacheFixtureRequest()) == nil {
				t.Fatal("cache pruning/bound failed")
			}
		})
	}
}

func TestWaitingRequestRevalidatesCache(t *testing.T) {
	endpoint, _ := cacheApprover(t, func(w http.ResponseWriter, r *http.Request) { t.Error("invalid cache reached Approver") })
	path, err := grantCachePath(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	os.Mkdir(GrantCacheDir(), 0700)
	lock, err := lockGrantCache(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	done := make(chan error, 1)
	go func() {
		_, err := RequestCachedGrant(context.Background(), endpoint, cacheFixtureRequest())
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(path, []byte("corrupt"), 0600)
	lock.Close()
	if err := <-done; err == nil {
		t.Fatal("cache corruption ignored after waiting")
	}
}

func TestRepositoryReusesWideGrantOffline(t *testing.T) {
	for _, scope := range []string{"organization", "account"} {
		t.Run(scope, func(t *testing.T) {
			request := Request{Permissions: map[string]string{"metadata": "read"}}
			approved := cacheFixtureGrant()
			approved.Repositories = nil
			approved.Permissions = request.Permissions
			if scope == "organization" {
				request.Organization, approved.Organization = "alice", "alice"
			} else {
				request.Account, approved.Account = "alice", "alice"
			}
			endpoint, server := cacheApprover(t, func(w http.ResponseWriter, r *http.Request) {
				json.NewEncoder(w).Encode(approved)
			})
			grant, err := RequestCachedGrant(context.Background(), endpoint, request)
			if err != nil {
				t.Fatal(err)
			}
			server.Close()
			repository := Request{Repositories: []string{"alice/repo"}, Permissions: map[string]string{"metadata": "read"}}
			reused, err := RequestCachedGrant(context.Background(), endpoint, repository)
			if err != nil || reused.Token != grant.Token || !reused.ExpiresAt.Equal(grant.ExpiresAt) || reused.Organization != grant.Organization || reused.Account != grant.Account {
				t.Fatal("wide grant was not reused unchanged", err)
			}
			repository.Permissions["contents"] = "read"
			if _, err := RequestCachedGrant(context.Background(), endpoint, repository); err == nil {
				t.Fatal("wide grant supplied unapproved contents access")
			}
		})
	}
}
