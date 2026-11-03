package main

import (
	"fmt"
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
