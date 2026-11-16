package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"passlet.dev/passlet/internal/passlet"
)

func pair(ctx context.Context, args []string) error {
	fs := commandFlags("pair", "Pair this Agent with an Approver using the one-time command printed by `passlet approver pair`. Approve pairing on your Mac with Touch ID. The endpoint and trusted key are saved locally for future requests.")
	code := fs.String("code", "", "single-use pairing code from your Mac")
	endpoint := fs.String("endpoint", "", "Approver endpoint (defaults to the local socket)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 || *code == "" {
		return errors.New("usage: passlet pair [--endpoint <endpoint>] --code <code>")
	}
	if os.Getenv("PASSLET_ENDPOINT") != "" {
		return errors.New("unset PASSLET_ENDPOINT before pairing")
	}
	if *endpoint == "" {
		*endpoint = "unix://" + passlet.DefaultSocket()
	}
	fmt.Println("Approve pairing on your Mac with Touch ID.")
	if err := passlet.PairAgent(ctx, *endpoint, *code, ""); err != nil {
		return err
	}
	fmt.Println("Paired with Approver:", *endpoint)
	return nil
}
