package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"

	"passlet.dev/passlet/internal/passlet"
)

type connectionError string

func (e connectionError) Error() string { return string(e) }

func connect(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Tell Passlet Agent how to connect to Passlet Approver. Run once on each Agent machine. The connection is saved in ~/.config/passlet/endpoint for future commands.\n\nLocal and SSH socket agents connect to `unix://$HOME/.local/share/passlet/passlet.sock` by default.\n\nUsage: passlet connect <https://host:port or unix:///absolute/socket>")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: passlet connect <https://host:port or unix:///absolute/socket>")
	}
	if os.Getenv("PASSLET_ENDPOINT") != "" {
		return connectionError("WARNING: PASSLET_ENDPOINT already selects an Approver.\nUnset PASSLET_ENDPOINT before running `passlet connect`.")
	}
	endpoint := fs.Arg(0)
	if _, err := passlet.EndpointURL(endpoint, "/v1/health"); err != nil {
		return err
	}
	if err := passlet.Health(ctx, endpoint); err != nil {
		return connectionError(fmt.Sprintf("WARNING: Could not connect to Approver: %s\nYour previous Approver connection has been kept.\n\nRun `passlet doctor --endpoint %s` for more information.", endpoint, endpoint))
	}
	if err := passlet.SaveEndpoint(endpoint); err != nil {
		return fmt.Errorf("could not save endpoint: %w", err)
	}
	fmt.Println("Connected to Approver:", endpoint)

	return nil
}
