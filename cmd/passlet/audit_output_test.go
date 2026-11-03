package main

import (
	"strings"
	"testing"

	"passlet.dev/passlet/internal/passlet"
)

func TestAuditHumanScopesAndControlCharacters(t *testing.T) {
	entries := []passlet.AuditEntry{
		{At: "2026-11-03T12:00:00Z", Event: passlet.Event{RequestID: "account", Outcome: "denied", Caller: passlet.Caller{Name: "remote\n\x1b[2J", Transport: "tailscale"}, Request: passlet.Request{Account: "alice", Permissions: map[string]string{"git_ssh_keys": "write"}}}},
		{At: "2026-11-03T11:00:00Z", Event: passlet.Event{RequestID: "org", Outcome: "approved", Request: passlet.Request{Organization: "acme"}}},
		{At: "2026-11-03T10:00:00Z", Event: passlet.Event{RequestID: "doctor", Operation: "doctor", Outcome: "verified"}},
	}
	output := formatAudit(entries)
	for _, text := range []string{"Agent: Shell", "Caller: remote��[2J (tailscale)", "Account: alice", "Organization: acme", "GitHub identity check", "\n\n2026-11-03T11:00:00Z"} {
		if !strings.Contains(output, text) {
			t.Fatalf("missing %q in %q", text, output)
		}
	}
	if strings.Contains(output, "\x1b") || strings.Contains(output, "remote\n") {
		t.Fatal("caller injected terminal controls")
	}
}
