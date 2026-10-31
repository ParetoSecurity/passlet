package passlet

import (
	_ "embed"
	"encoding/json"
	"errors"
	"sort"
	"strings"
)

type permissionSpec struct {
	Scope  string   `json:"scope"`
	Levels []string `json:"levels"`
}

//go:embed github_permissions.json
var permissionJSON []byte
var permissionCatalog = func() map[string]permissionSpec {
	var catalog struct {
		Permissions map[string]permissionSpec `json:"permissions"`
	}
	if err := json.Unmarshal(permissionJSON, &catalog); err != nil {
		panic(err)
	}
	return catalog.Permissions
}()

func validPermission(name, level string) bool {
	for _, allowed := range permissionCatalog[name].Levels {
		if allowed == level {
			return true
		}
	}
	return false
}
func permissionCovers(name, actual, requested string) bool {
	levels := permissionCatalog[name].Levels
	a, r := -1, -1
	for i, level := range levels {
		if level == actual {
			a = i
		}
		if level == requested {
			r = i
		}
	}
	return r >= 0 && a >= r
}

// The token API schema is not a manifest-registration schema. Keep known
// GitHub.com registration incompatibilities separate from token capabilities.
// https://github.com/orgs/community/discussions/32195
var manifestExcludedPermissions = map[string]string{
	"organization_packages": "not_available_for_GitHub_com_App_registration",
}

var DefaultSetupPermissions = func() string {
	parts := []string{}
	for name, spec := range permissionCatalog {
		if _, excluded := manifestExcludedPermissions[name]; excluded {
			continue
		}
		parts = append(parts, name+":"+spec.Levels[len(spec.Levels)-1])
	}
	sort.Strings(parts)
	return strings.Join(parts, ",")
}()

// GitHub's registration form uses different names from its token API for
// these account permissions. Keep the API names internally.
func manifestPermissions(permissions map[string]string) map[string]string {
	result := make(map[string]string, len(permissions))
	for name, level := range permissions {
		switch name {
		case "email_addresses":
			name = "emails"
		case "git_ssh_keys":
			name = "keys"
		}
		result[name] = level
	}
	return result
}

// Manifest conversion can return registration names; normalize them before
// comparing permissions or checking installation access. Reject collisions.
func normalizeRegistrationPermissions(permissions map[string]string) (map[string]string, error) {
	result := make(map[string]string, len(permissions))
	for name, level := range permissions {
		switch name {
		case "emails":
			name = "email_addresses"
		case "keys":
			name = "git_ssh_keys"
		}
		if _, exists := result[name]; exists {
			return nil, errors.New("app_registration_duplicate_permission")
		}
		result[name] = level
	}
	return result, nil
}
