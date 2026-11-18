package passlet

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const maxCachedGrants = 128

func GrantCacheDir() string { return filepath.Join(StateDir(), "grants") }

func grantCovers(g Grant, r Request) bool {
	if g.ID == "" || g.Token == "" || g.User == "" || time.Until(g.ExpiresAt) < time.Minute || time.Until(g.ExpiresAt) > 8*time.Hour+time.Minute {
		return false
	}
	if r.Organization != "" && g.Organization != r.Organization || r.Account != "" && g.Account != r.Account {
		return false
	}
	for _, repo := range r.Repositories {
		owner := strings.SplitN(repo, "/", 2)[0]
		if !slices.Contains(g.Repositories, repo) && g.Organization != owner && g.Account != owner {
			return false
		}
	}
	for name, level := range r.Permissions {
		if permissionLevel(g.Permissions[name]) < permissionLevel(level) {
			return false
		}
	}
	return true
}

func permissionLevel(level string) int {
	return slices.Index([]string{"", "read", "write", "admin"}, level)
}

func grantBreadth(g Grant) int {
	score := len(g.Repositories) * 32
	if g.Organization != "" || g.Account != "" {
		score += 101 * 32
	}
	for _, level := range g.Permissions {
		score += permissionLevel(level)
	}
	return score
}

func grantCacheIdentity(endpoint string) (string, error) {
	if _, err := endpointURL(endpoint); err != nil {
		return "", err
	}
	connection, err := readConnection()
	if err != nil {
		return "", err
	}
	private, err := ReadPrivateKey(filepath.Join(StateDir(), "agent.pem"))
	if err != nil {
		return "", err
	}
	return endpoint + "\n" + connection.Approver + "\n" + keyID(private.Public().(ed25519.PublicKey)), nil
}

func grantCachePath(endpoint string) (string, error) {
	identity, err := grantCacheIdentity(endpoint)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(identity))
	return filepath.Join(GrantCacheDir(), hex.EncodeToString(digest[:])+".json"), nil
}

func openCacheFile(path string, flags int) (*os.File, error) {
	fd, err := syscall.Open(path, flags|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0600)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || info.Sys().(*syscall.Stat_t).Uid != uint32(os.Getuid()) || info.Sys().(*syscall.Stat_t).Nlink > 1 {
		_ = file.Close()
		return nil, errors.New("grant_cache_file_requires_owner_and_mode_0600")
	}
	return file, nil
}

func readCachedGrants(path string) ([]Grant, error) {
	file, err := openCacheFile(path, syscall.O_RDONLY)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	const limit = maxCachedGrants * 65536
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil { // coverage-ignore: Requires filesystem failure while reading a verified regular file.
		return nil, err
	}
	var grants []Grant
	if len(data) > limit || strictJSON(data, &grants) != nil || len(grants) > maxCachedGrants {
		return nil, errors.New("invalid_grant_cache")
	}
	return grants, nil
}

func cachedMatch(grants []Grant, request Request) *Grant {
	var best *Grant
	for i := range grants {
		grant := &grants[i]
		if grantCovers(*grant, request) && (best == nil || grantBreadth(*grant) < grantBreadth(*best)) {
			best = grant
		}
	}
	return best
}

func lockGrantCache(ctx context.Context, path string) (*os.File, error) {
	// Keep the lock inode stable across atomic cache replacements and processes.
	file, err := openCacheFile(path+".lock", syscall.O_RDWR|syscall.O_CREAT)
	if err != nil {
		return nil, err
	}
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) { // coverage-ignore: Requires locking to fail on a verified regular file for a reason other than contention.
			_ = file.Close()
			return nil, err
		}
		select {
		case <-ctx.Done():
			_ = file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}

func RequestCachedGrant(ctx context.Context, endpoint string, request Request) (Grant, error) {
	if err := request.Validate(); err != nil {
		return Grant{}, err
	}
	path, err := grantCachePath(endpoint)
	if err != nil {
		return Grant{}, err
	}
	for _, dir := range []string{StateDir(), GrantCacheDir()} {
		if err := PrivateDir(dir); err != nil {
			return Grant{}, err
		}
	}
	grants, err := readCachedGrants(path)
	if err != nil {
		return Grant{}, err
	}
	if grant := cachedMatch(grants, request); grant != nil {
		return *grant, nil
	}
	ctx, cancel := context.WithTimeout(ctx, ClientDeadline)
	defer cancel()
	lock, err := lockGrantCache(ctx, path)
	if err != nil {
		return Grant{}, err
	}
	defer func() { _ = lock.Close() }()
	// Another session may have obtained approval while this request waited.
	grants, err = readCachedGrants(path)
	if err != nil {
		return Grant{}, err
	}
	if grant := cachedMatch(grants, request); grant != nil {
		return *grant, nil
	}
	approved, err := RequestGrant(ctx, endpoint, request)
	if err != nil {
		return Grant{}, err
	}
	current, err := grantCachePath(endpoint)
	if err != nil || current != path {
		return Grant{}, errors.New("pairing_changed_during_request")
	}
	grants = slices.DeleteFunc(grants, func(grant Grant) bool { return !grant.ExpiresAt.After(time.Now()) })
	if len(grants) == maxCachedGrants {
		grants = slices.Delete(grants, 0, 1)
	}
	grants = append(grants, approved)
	data, _ := json.Marshal(grants)
	if err := writePairingKey(path, data, true); err != nil {
		return Grant{}, err
	}
	return approved, nil
}

// DiscardRejectedGrant checks authentication separately because a child failure
// can reflect missing permissions or a partial write, not a rejected token.
func DiscardRejectedGrant(ctx context.Context, endpoint string, grant Grant) error {
	client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/user", nil)
	if err != nil { // coverage-ignore: The URL and method are constants and callers supply a non-nil context.
		return err
	}
	request.Header.Set("Authorization", "Bearer "+grant.Token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("User-Agent", "Passlet")
	response, err := client.Do(request)
	if err != nil {
		return nil
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		return nil
	}
	return discardCachedGrant(ctx, endpoint, grant)
}

func discardCachedGrant(ctx context.Context, endpoint string, rejected Grant) error {
	path, err := grantCachePath(endpoint)
	if err != nil {
		return err
	}
	for _, dir := range []string{StateDir(), GrantCacheDir()} {
		if err := CheckPrivateDir(dir); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(ctx, ClientDeadline)
	defer cancel()
	lock, err := lockGrantCache(ctx, path)
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()
	grants, err := readCachedGrants(path)
	if err != nil {
		return err
	}
	count := len(grants)
	grants = slices.DeleteFunc(grants, func(grant Grant) bool { return grant.Token == rejected.Token })
	if len(grants) == count {
		return nil
	}
	data, _ := json.Marshal(grants)
	return writePairingKey(path, data, true)
}
