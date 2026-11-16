//go:build agentonly

package main

import (
	"context"
	"errors"
)

const commandHelp = `Give your remote agents scoped access to services, approved on-the-fly from your Mac.

Run ` + "`passlet approver`" + ` on your Mac to manage approvals and credentials.
Your Agents then run ` + "`passlet request`" + `.

Usage: passlet <command>

Commands:
  doctor        Check your connection and verify GitHub access
  exec          Run a command with scoped access
  pair          Pair with an Approver
  request       Request scoped access
  ssh-agent     Forward your Mac's SSH agent on demand

Run ` + "`passlet <command> --help`" + ` for command options.
`

var overview func(context.Context) error

func approverCommand(context.Context, []string, func(context.Context) error) error {
	return errors.New("approver_requires_the_macOS_build_of_Passlet")
}
