package main

import (
	"context"
	"os/exec"
	"strings"
	"time"

	"passlet.dev/passlet/internal/passlet"
)

// Read explicit account metadata only. Never call auth status, obtain a token,
// or infer a GitHub login from Git's author name/email.
func configuredGitHubHandle(ctx context.Context) string {
	return suggestGitHubHandle(func(program string, args ...string) string {
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, program, args...).Output()
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(out))
	})
}

func suggestGitHubHandle(read func(string, ...string) string) string {
	for _, command := range [][]string{
		{"git", "config", "--get", "github.user"},
		{"gh", "config", "get", "user", "--host", "github.com"},
	} {
		handle := strings.TrimSpace(read(command[0], command[1:]...))
		if _, err := passlet.GitHubAppName(handle); err == nil {
			return strings.TrimPrefix(handle, "@")
		}
	}
	return ""
}
