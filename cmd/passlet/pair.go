package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"slices"
	"time"

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

func approverPair(args []string) error {
	fs := flag.NewFlagSet("approver pair", flag.ContinueOnError)
	fs.Usage = func() {
		_, _ = fmt.Fprintln(fs.Output(), "Create a pairing code for one Agent. The code expires after 10 minutes. Pairing requires Touch ID and does not access stored credentials.")
	}
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: passlet approver pair")
	}
	code, err := passlet.CreateInvitation()
	if err != nil {
		return err
	}
	config, err := configuredApprover()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	addresses, problem := overviewRemoteAddresses(ctx, config.Tailscale, overviewOutput)
	fmt.Println("For an Agent using the default socket (local or SSH-forwarded), run:")
	fmt.Printf("  passlet pair --code %s\n\n", code)
	if len(addresses) > 0 {
		fmt.Println("For an Agent connecting through Tailscale, run:")
		fmt.Printf("  passlet pair --endpoint %s --code %s\n\n", addresses[0], code)
	}
	if problem != "" {
		fmt.Printf("WARNING: Tailscale pairing instructions are unavailable.\n%s\n\n", problem)
	}
	fmt.Println("For a different SSH-forwarded socket, use `--endpoint unix:///path/to/socket`.")
	fmt.Println("The code expires in 10 minutes and works once. Approve pairing on this Mac with Touch ID.")
	return nil
}

func approverAgents(args []string) error {
	if len(args) == 1 && args[0] == "--help" {
		fmt.Println("List paired Agents or revoke access.\n\nUsage: passlet approver agents list\n       passlet approver agents revoke <name>")
		return nil
	}
	if len(args) == 1 && args[0] == "list" {
		agents, err := passlet.PairingAgents()
		if err != nil {
			return err
		}
		names := make([]string, 0, len(agents))
		for _, name := range agents {
			names = append(names, name)
		}
		slices.Sort(names)
		if len(names) == 0 {
			fmt.Println("No paired Agents.")
		}
		for _, name := range names {
			fmt.Println(name)
		}
		return nil
	}
	if len(args) == 2 && args[0] == "revoke" {
		if err := passlet.RevokeAgent(args[1]); err != nil {
			return err
		}
		fmt.Println("Revoked Agent:", args[1])
		return nil
	}
	return errors.New("usage: passlet approver agents list | revoke <name>")
}
