//go:build !agentonly

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"passlet.dev/passlet/internal/passlet"
)

func formatAudit(entries []passlet.AuditEntry) string {
	if len(entries) == 0 {
		return "No audit events.\n"
	}
	blocks := make([]string, 0, len(entries))
	for _, entry := range entries {
		event := entry.Event
		scope := "Repositories: " + strings.Join(event.Request.Repositories, ", ")
		switch {
		case event.Operation == "doctor":
			scope = "GitHub identity check"
		case event.Request.Account != "":
			scope = "Account: " + event.Request.Account
		case event.Request.Organization != "":
			scope = "Organization: " + event.Request.Organization
		}
		permissions := make([]string, 0, len(event.Request.Permissions))
		for name, level := range event.Request.Permissions {
			permissions = append(permissions, name+": "+level)
		}
		sort.Strings(permissions)
		agent := event.Request.Agent
		if agent == "" {
			agent = "Shell"
		}
		blocks = append(blocks, fmt.Sprintf("%s  %s\n  Request: %s\n  Agent: %s\n  Caller: %s (%s)\n  %s\n  Requested permissions: %s",
			auditText(entry.At), auditText(event.Outcome), auditText(event.RequestID), auditText(agent),
			auditText(event.Caller.Name), auditText(event.Caller.Transport), auditText(scope), auditText(strings.Join(permissions, ", "))))
	}
	return strings.Join(blocks, "\n\n") + "\n"
}

func auditText(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '�'
		}
		return r
	}, value)
}

func auditCommand(args []string) error {
	fs := commandFlags("approver audit", "Show recent access events from the local audit log.")
	jsonOutput := fs.Bool("json", false, "print structured records as JSON")
	limit := fs.Int("limit", 20, "recent events, 1–1000")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("unexpected_arguments")
	}
	entries, skipped, err := passlet.ReadAudit(filepath.Join(passlet.StateDir(), "audit.sqlite"), *limit)
	if err != nil {
		return err
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "Warning: audit log contains corrupted entries (%d skipped).\n", skipped)
	}
	if *jsonOutput {
		return json.NewEncoder(os.Stdout).Encode(entries)
	}
	_, err = fmt.Fprint(os.Stdout, formatAudit(entries))
	return err
}
