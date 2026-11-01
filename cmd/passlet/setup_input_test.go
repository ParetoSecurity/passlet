package main

import (
	"bufio"
	"bytes"
	"strings"
	"testing"
)

func TestSetupReadLine(t *testing.T) {
	for _, tc := range []struct {
		name, input, want string
		fail              bool
	}{
		{"accept suggestion", "\n", "alice", false},
		{"override suggestion", " bob \n", "bob", false},
		{"EOF is not confirmation", "", "", true},
		{"unterminated input is not confirmation", "bob", "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			got, err := setupReadLine(setupDependencies{input: bufio.NewReader(strings.NewReader(tc.input)), output: &output}, "Handle: ", "alice")
			if (err != nil) != tc.fail || got != tc.want {
				t.Fatalf("got %q, %v", got, err)
			}
			if output.String() != "Handle: " {
				t.Fatalf("prompt: %q", output.String())
			}
		})
	}
}
