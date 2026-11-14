package passlet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type cloudflareResource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type cloudflareCatalog struct {
	Accounts map[string]string `json:"accounts"`
	Zones    map[string]string `json:"zones"`
}

func cloudflareCatalogPath() string {
	return filepath.Join(StateDir(), "cloudflare-resources.json")
}

func (c *Cloudflare) RefreshResources(ctx context.Context, credentials Credentials) error {
	if credentials.Provider != "cloudflare" || credentials.Validate() != nil {
		return errors.New("invalid_cloudflare_credentials")
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	accounts, err := c.discoverResources(ctx, credentials.AccessToken, "com.cloudflare.api.account", "Account Settings Read", "/accounts")
	if err != nil {
		return err
	}
	zones, err := c.discoverResources(ctx, credentials.AccessToken, "com.cloudflare.api.account.zone", "Zone Read", "/zones")
	if err != nil {
		return err
	}
	data, _ := json.Marshal(cloudflareCatalog{Accounts: accounts, Zones: zones})
	return writePairingKey(cloudflareCatalogPath(), data, true)
}

func (c *Cloudflare) discoverResources(ctx context.Context, parent, scope, permission, path string) (_ map[string]string, err error) {
	var groups []cloudflareGroup
	if err := c.call(ctx, parent, "GET", "/user/tokens/permission_groups", nil, &groups); err != nil {
		return nil, err
	}
	selected := []cloudflareGroup{}
	for _, group := range groups {
		if group.Name == permission && slices.Contains(group.Scopes, scope) && cloudflareID.MatchString(group.ID) {
			selected = append(selected, cloudflareGroup{ID: group.ID})
		}
	}
	if len(selected) != 1 {
		return nil, errors.New("cloudflare_resource_discovery_permission_unavailable")
	}
	policies := []cloudflarePolicy{{Effect: "allow", Resources: map[string]string{scope + ".*": "*"}, Groups: selected}}
	expiry := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Second)
	body := struct {
		Name      string             `json:"name"`
		Policies  []cloudflarePolicy `json:"policies"`
		ExpiresAt time.Time          `json:"expires_on"`
	}{"Passlet resource discovery", policies, expiry}
	var issued cloudflareToken
	if err := c.call(ctx, parent, "POST", "/user/tokens", body, &issued); err != nil {
		return nil, err
	}
	if cloudflareID.MatchString(issued.ID) {
		defer func() {
			// Revoke even when discovery was cancelled by the caller.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
			defer cancel()
			var result json.RawMessage
			if revokeErr := c.call(cleanup, parent, "DELETE", "/user/tokens/"+issued.ID, nil, &result); revokeErr != nil {
				err = errors.New("cloudflare_discovery_token_revocation_failed")
			}
		}()
	}
	if !cloudflareID.MatchString(issued.ID) || issued.Value == "" || issued.Status != "active" || !issued.ExpiresAt.After(time.Now()) || issued.ExpiresAt.After(expiry) || !sameCloudflarePolicies(issued.Policies, policies) {
		return nil, errors.New("unexpected_cloudflare_token_scope_or_expiry")
	}
	resources := map[string]string{}
	for page := 1; ; page++ {
		var items []cloudflareResource
		if err := c.call(ctx, issued.Value, "GET", fmt.Sprintf("%s?per_page=50&page=%d", path, page), nil, &items); err != nil {
			return nil, err
		}
		for _, item := range items {
			if !cloudflareID.MatchString(item.ID) || item.Name == "" || resources[item.ID] != "" {
				return nil, errors.New("invalid_cloudflare_resource")
			}
			resources[item.ID] = item.Name
		}
		if len(items) < 50 {
			return resources, nil
		}
	}
}

func loadCloudflareCatalog() cloudflareCatalog {
	var catalog cloudflareCatalog
	file, err := os.Open(cloudflareCatalogPath())
	if err != nil {
		return catalog
	}
	defer func() { _ = file.Close() }()
	if json.NewDecoder(io.LimitReader(file, 1048576)).Decode(&catalog) != nil {
		return cloudflareCatalog{}
	}
	return catalog
}

func cloudflareResourceLabel(id string, names map[string]string) string {
	name := strings.TrimSpace(safeLabel(names[id]))
	if name == "" {
		return id
	}
	return id + " (" + name + ", cached)"
}
