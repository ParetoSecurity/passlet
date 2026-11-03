package main

import (
	"flag"
	"fmt"
	"slices"
	"strings"
)

func commandFlags(name, description string, hidden ...string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintf(fs.Output(), "%s\n\nUsage of %s:\n", description, name)
		output := fs.Output()
		var defaults strings.Builder
		visible := flag.NewFlagSet(name, flag.ContinueOnError)
		visible.SetOutput(&defaults)
		fs.VisitAll(func(f *flag.Flag) {
			if !slices.Contains(hidden, f.Name) {
				visible.Var(f.Value, f.Name, f.Usage)
				visible.Lookup(f.Name).DefValue = f.DefValue
			}
		})
		visible.PrintDefaults()
		_, _ = fmt.Fprint(output, strings.TrimPrefix(strings.ReplaceAll("\n"+defaults.String(), "\n  -", "\n  --"), "\n"))
	}
	return fs
}
