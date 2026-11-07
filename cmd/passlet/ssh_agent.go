package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"passlet.dev/passlet/internal/passlet"
)

func sshAgent(ctx context.Context, args []string) error {
	fs := commandFlags("ssh-agent", "Provide a private SSH-agent socket for this machine. Requests are forwarded on demand to your paired Approver. Configure the upstream SSH-agent socket on your Approver Mac.", "endpoint", "stdio")
	stdio := fs.Bool("stdio", false, "serve one socket connection supplied on standard input")
	endpoint := fs.String("endpoint", "", "override the paired Approver endpoint")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 0 {
		return errors.New("usage: passlet ssh-agent")
	}
	target, err := passlet.ResolveEndpoint(*endpoint)
	if err != nil {
		return err
	}
	if _, err := passlet.EndpointURL(target, "/v1/ssh-agent"); err != nil {
		return err
	}
	if *stdio {
		conn, err := net.FileConn(os.Stdin)
		if err != nil {
			return errors.New("SSH-agent socket activation requires a connected socket on standard input")
		}
		passlet.ServeSSHAgentConnection(ctx, conn, target)
		return nil
	}
	listener, err := passlet.ListenUnix(passlet.SSHAgentSocket())
	if err != nil {
		return err
	}
	defer func() { _ = listener.Close() }()
	fmt.Println("SSH-agent socket: " + passlet.SSHAgentSocket())
	fmt.Println("Set SSH_AUTH_SOCK to this path in your shell or Rex service environment.")
	return passlet.ServeSSHAgent(ctx, listener, target)
}
