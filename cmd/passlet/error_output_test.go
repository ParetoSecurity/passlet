package main

import (
	"bytes"
	"errors"
	"testing"
)

func TestCommandErrorOutput(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"explained Agent setup", errSetupOnAgent, ""},
		{"explained app conflict", errGitHubAppNameTaken, "\n"},
		{"unexpected error", errors.New("connection failed"), "passlet: connection failed\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			writeCommandError(&out, tc.err)
			if out.String() != tc.want {
				t.Fatalf("error output: got %q, want %q", out.String(), tc.want)
			}
		})
	}
}
