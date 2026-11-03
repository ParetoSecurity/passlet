package main

import "testing"

func TestInferAgentName(t *testing.T) {
	for _, tc := range []struct{ name, claude, codex, want string }{
		{"shell", "", "", "Shell"},
		{"claude", "1", "", "Claude"},
		{"codex", "", "fixture-thread", "Codex"},
		{"ambiguous", "1", "fixture-thread", "Shell"},
		{"disabled claude", "0", "", "Shell"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("CLAUDECODE", tc.claude)
			t.Setenv("CODEX_THREAD_ID", tc.codex)
			if got := inferAgentName(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}
