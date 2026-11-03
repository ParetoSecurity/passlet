package main

import "os"

func inferAgentName() string {
	claude := os.Getenv("CLAUDECODE") == "1"
	codex := os.Getenv("CODEX_THREAD_ID") != ""
	switch {
	case claude && !codex:
		return "Claude"
	case codex && !claude:
		return "Codex"
	default:
		return "Shell"
	}
}
