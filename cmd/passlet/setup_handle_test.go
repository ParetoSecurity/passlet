package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfiguredHandleExecutesOnlyMetadataCommands(t *testing.T) {
	dir := t.TempDir()
	for name, script := range map[string]string{
		"git": "#!/bin/sh\n[ \"$*\" = 'config --get github.user' ] || exit 2\nexit 1\n",
		"gh":  "#!/bin/sh\n[ \"$*\" = 'config get user --host github.com' ] || exit 2\nprintf '  alice\\n'\n",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir)
	if got := configuredGitHubHandle(context.Background()); got != "alice" {
		t.Fatalf("fallback handle: %q", got)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := configuredGitHubHandle(ctx); got != "" {
		t.Fatalf("cancelled lookup returned %q", got)
	}
}

func TestSuggestGitHubHandle(t *testing.T) {
	for _, tc := range []struct {
		name, git, gh, want string
		calls               int
	}{
		{"prefer Git", "alice", "bob", "alice", 1},
		{"Git only", "alice", "", "alice", 1},
		{"GitHub CLI fallback", "", "alice", "alice", 2},
		{"neither configured", "", "", "", 2},
		{"invalid Git value", "Alice Smith", "alice", "alice", 2},
		{"invalid account metadata", "", "bad/user", "", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got := suggestGitHubHandle(func(program string, args ...string) string {
				calls++
				switch program + " " + strings.Join(args, " ") {
				case "git config --get github.user":
					return tc.git
				case "gh config get user --host github.com":
					return tc.gh
				default:
					t.Fatal("unexpected command: handle lookup must not use tokens")
					return ""
				}
			})
			if got != tc.want || calls != tc.calls {
				t.Fatalf("got %q (%d lookups), want %q (%d)", got, calls, tc.want, tc.calls)
			}
		})
	}
}
