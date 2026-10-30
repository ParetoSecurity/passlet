package passlet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type GitHub struct {
	HTTP  *http.Client
	API   string
	OAuth string
}

func NewGitHub() *GitHub {
	return &GitHub{HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, API: "https://api.github.com", OAuth: "https://github.com/login/oauth/access_token"}
}
func (g *GitHub) call(ctx context.Context, method, path, token string, basic *Credentials, body any, out any) error {
	var data []byte
	var err error
	if body != nil {
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, g.API+path, bytes.NewReader(data))
	if err != nil {
		return errors.New("github_request_failed")
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2026-03-10")
	req.Header.Set("User-Agent", "Passlet")
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if basic != nil {
		req.SetBasicAuth(basic.ClientID, basic.ClientSecret)
	}
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return errors.New("github_unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("github_http_%d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out); err != nil {
		return errors.New("github_invalid_response")
	}
	return nil
}
func (g *GitHub) Refresh(ctx context.Context, c Credentials) (Credentials, error) {
	if time.Until(c.ExpiresAt) > 2*time.Minute {
		return c, nil
	}
	form := url.Values{"client_id": {c.ClientID}, "client_secret": {c.ClientSecret}, "grant_type": {"refresh_token"}, "refresh_token": {c.RefreshToken}}
	req, err := http.NewRequestWithContext(ctx, "POST", g.OAuth, strings.NewReader(form.Encode()))
	if err != nil {
		return c, errors.New("refresh_failed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.HTTP.Do(req)
	if err != nil {
		return c, errors.New("refresh_failed")
	}
	defer resp.Body.Close()
	var v struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&v) != nil || v.AccessToken == "" || v.RefreshToken == "" || v.ExpiresIn <= 0 || v.ExpiresIn > 28800 {
		return c, errors.New("reauthorization_required")
	}
	c.AccessToken = v.AccessToken
	c.RefreshToken = v.RefreshToken
	c.ExpiresAt = time.Now().UTC().Add(time.Duration(v.ExpiresIn) * time.Second)
	return c, nil
}
func (g *GitHub) Mint(ctx context.Context, c Credentials, r Request) (Grant, error) {
	if err := r.Validate(); err != nil {
		return Grant{}, err
	}
	owner := strings.Split(r.Repositories[0], "/")[0]
	var user struct {
		Login string `json:"login"`
	}
	if err := g.call(ctx, "GET", "/user", c.AccessToken, nil, nil, &user); err != nil {
		return Grant{}, err
	}
	if user.Login == "" {
		return Grant{}, errors.New("unexpected_user")
	}
	// Reject repositories absent from the installation before calling the scoping
	// endpoint: GitHub can ignore invalid repository selectors in some token flows.
	installationID := int64(0)
	for page := 1; page <= 100; page++ {
		var installations struct {
			Items []struct {
				ID      int64 `json:"id"`
				Account struct {
					Login string `json:"login"`
				} `json:"account"`
			} `json:"installations"`
		}
		if err := g.call(ctx, "GET", "/user/installations?per_page=100&page="+strconv.Itoa(page), c.AccessToken, nil, nil, &installations); err != nil {
			return Grant{}, err
		}
		for _, i := range installations.Items {
			if strings.EqualFold(i.Account.Login, owner) {
				installationID = i.ID
			}
		}
		if installationID != 0 || len(installations.Items) < 100 {
			break
		}
	}
	if installationID == 0 {
		return Grant{}, errors.New("repository_not_installed")
	}
	wanted := map[string]int64{}
	for _, repo := range r.Repositories {
		wanted[repo] = 0
	}
	for page := 1; page <= 100; page++ {
		var repos struct {
			Items []struct {
				ID   int64  `json:"id"`
				Name string `json:"full_name"`
			} `json:"repositories"`
		}
		path := fmt.Sprintf("/user/installations/%d/repositories?per_page=100&page=%d", installationID, page)
		if err := g.call(ctx, "GET", path, c.AccessToken, nil, nil, &repos); err != nil {
			return Grant{}, err
		}
		for _, repo := range repos.Items {
			name := strings.ToLower(repo.Name)
			if _, ok := wanted[name]; ok {
				wanted[name] = repo.ID
			}
		}
		if len(repos.Items) < 100 {
			break
		}
	}
	ids := []int64{}
	for _, repo := range r.Repositories {
		if wanted[repo] == 0 {
			return Grant{}, errors.New("repository_not_installed")
		}
		ids = append(ids, wanted[repo])
	}
	var minted struct {
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
		User      struct {
			Login string `json:"login"`
		} `json:"user"`
		Installation struct {
			Permissions map[string]string `json:"permissions"`
			Selection   string            `json:"repository_selection"`
			Account     struct {
				Login string `json:"login"`
			} `json:"account"`
		} `json:"installation"`
	}
	body := map[string]any{"access_token": c.AccessToken, "target": owner, "repository_ids": ids, "permissions": r.Permissions}
	if err := g.call(ctx, "POST", "/applications/"+url.PathEscape(c.ClientID)+"/token/scoped", "", &c, body, &minted); err != nil {
		return Grant{}, err
	}
	if minted.Token == "" || minted.Token == c.AccessToken || minted.Token == c.RefreshToken || minted.Token == c.ClientSecret || minted.User.Login != user.Login || minted.Installation.Selection != "selected" || !strings.EqualFold(minted.Installation.Account.Login, owner) {
		return Grant{}, errors.New("unexpected_grant")
	}
	if len(minted.Installation.Permissions) != len(r.Permissions) {
		return Grant{}, errors.New("unexpected_permissions")
	}
	for k, v := range r.Permissions {
		if minted.Installation.Permissions[k] != v {
			return Grant{}, errors.New("unexpected_permissions")
		}
	}
	if !minted.ExpiresAt.After(time.Now()) || time.Until(minted.ExpiresAt) > 8*time.Hour+time.Minute {
		return Grant{}, errors.New("unexpected_expiry")
	}
	return Grant{Token: minted.Token, ExpiresAt: minted.ExpiresAt, Repositories: r.Repositories, Permissions: minted.Installation.Permissions, User: user.Login}, nil
}
