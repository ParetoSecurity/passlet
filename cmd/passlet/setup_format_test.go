package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetupFormatting(t *testing.T) {
	for _, color := range []bool{false, true} {
		var out bytes.Buffer
		url := "https://github.com/" + strings.Repeat("x", 100)
		text := "This GitHub App is yours alone. Touch ID\n\n" + strings.Repeat("word ", 20) + "last\n" + url + "\nPress Enter to continue.\nGitHub setup complete."
		writeFormattedSetupText(&out, text, color)
		got := out.String()
		if strings.Contains(got, "\x1b[") != color {
			t.Fatalf("color=%t: %q", color, got)
		}
		plain := strings.ReplaceAll(strings.ReplaceAll(got, "\x1b[1m", ""), "\x1b[0m", "")
		want := "This GitHub App is yours alone. Touch ID\n\n" + strings.TrimSpace(strings.Repeat("word ", 16)) + "\nword word word word last\n" + url + "\nPress Enter to continue.\nGitHub setup complete.\n"
		if plain != want {
			t.Fatalf("formatting:\ngot %q\nwant %q", plain, want)
		}
		if color && (!strings.Contains(got, "\x1b[1mTouch ID\x1b[0m") || !strings.Contains(got, "\x1b[1mPress Enter to continue.\x1b[0m")) {
			t.Fatalf("missing emphasis: %q", got)
		}
	}
}

func TestRedirectedSetupTextIsPlain(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writeSetupText(file, "Touch ID\nPress Enter to continue.")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "\nTouch ID\nPress Enter to continue.\n" {
		t.Fatalf("redirected output: %q", got)
	}
}
