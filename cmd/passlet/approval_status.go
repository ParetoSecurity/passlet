package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"syscall"

	"passlet.dev/passlet/internal/passlet"
)

func rexTerminal() io.WriteCloser { // coverage-ignore: OS terminal adapter, verified in Rex with redirected stdout and stderr.
	if os.Getenv("REX_SESSION") == "" {
		return nil
	}
	terminal, err := os.OpenFile("/dev/tty", os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil
	}
	return terminal
}

func requestGrantWithStatus(ctx context.Context, endpoint string, requested passlet.Request, terminal io.WriteCloser) (passlet.Grant, error) {
	if terminal != nil {
		defer func() { _ = terminal.Close() }()
	}
	if err := requested.Validate(); err != nil {
		return passlet.Grant{}, err
	}
	if terminal != nil {
		id := fmt.Sprintf("passlet-%d", os.Getpid())
		message := base64.StdEncoding.EncodeToString([]byte(approvalMessage(requested)))
		// A separate record preserves the coding agent's own program status.
		_, _ = fmt.Fprintf(terminal, "\x1b]7501;id=%s:state=blocked:kind=permission:app=passlet:msg=%s\x1b\\", id, message)
		defer func() {
			_, _ = fmt.Fprintf(terminal, "\x1b]7501;id=%s:state=clear\x1b\\", id)
		}()
	}
	return passlet.RequestGrant(ctx, endpoint, requested)
}

func approvalMessage(requested passlet.Request) string {
	target := "organization " + requested.Organization
	if requested.Account != "" {
		target = "account " + requested.Account
	}
	if len(requested.Repositories) > 0 {
		target = requested.Repositories[0]
		if len(requested.Repositories) > 1 {
			target += fmt.Sprintf(" and %d more", len(requested.Repositories)-1)
		}
	}
	permissions := make([]string, 0, len(requested.Permissions))
	for name, level := range requested.Permissions {
		if name != "metadata" {
			permissions = append(permissions, name+":"+level)
		}
	}
	sort.Strings(permissions)
	return "Waiting for approval: " + target + " (" + strings.Join(permissions, ", ") + ")"
}
