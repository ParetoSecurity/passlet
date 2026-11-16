//go:build agentonly

package main

import (
	"os"
	"strings"
	"testing"
)

func TestAgentOnlyBuildHasNoApprover(t *testing.T) {
	if strings.Contains(commandHelp, "\n  approver") {
		t.Fatal("help lists the approver command")
	}
	args := os.Args
	t.Cleanup(func() { os.Args = args })
	for _, command := range []string{"start", "doctor"} {
		os.Args = []string{"passlet", "approver", command}
		if err := run(); err == nil || err.Error() != "approver_requires_the_macOS_build_of_Passlet" {
			t.Fatalf("approver %s: got %v", command, err)
		}
	}
}
