package main

import (
	"fmt"
	"io"
	"os"
	"strings"
)

func writeSetupIntroduction(out io.Writer) {
	writeSetupText(out, setupIntroduction)
}

func writeSetupText(out io.Writer, text string) { // coverage-ignore: Terminal/file adapter needs a real TTY; formatting and plain redirected output are tested independently.
	_, _ = fmt.Fprintln(out)
	file, isFile := out.(*os.File)
	if !isFile {
		_, _ = fmt.Fprintln(out, text)
		return
	}
	info, err := file.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 || os.Getenv("TERM") == "dumb" {
		_, _ = fmt.Fprintln(out, text)
		return
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	writeFormattedSetupText(out, text, !noColor)
}

func writeFormattedSetupText(out io.Writer, text string, color bool) {
	paragraphs := strings.Split(text, "\n")
	for _, paragraph := range paragraphs {
		if strings.HasPrefix(paragraph, "http://") || strings.HasPrefix(paragraph, "https://") {
			_, _ = fmt.Fprintln(out, paragraph)
			continue
		}
		// Keep paragraphs readable even in wide terminals, breaking at word boundaries.
		var lines []string
		line := ""
		for _, word := range strings.Fields(paragraph) {
			if line != "" && len([]rune(line))+1+len([]rune(word)) > 80 {
				lines = append(lines, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		lines = append(lines, line)
		text := strings.Join(lines, "\n")
		if color {
			if strings.HasPrefix(paragraph, "Press Enter") || paragraph == "GitHub setup complete." {
				text = "\x1b[1m" + text + "\x1b[0m"
			} else {
				for _, phrase := range []string{"This GitHub App is yours alone.", "Touch ID", "All repositories", "Verified:", "App:", "Account:"} {
					text = strings.ReplaceAll(text, phrase, "\x1b[1m"+phrase+"\x1b[0m")
				}
			}
		}
		_, _ = fmt.Fprintln(out, text)
	}
}
