Feature: Show command help

  Rule: Passlet commands

    Scenario: Version is printed
      When I run `passlet --version`
      Then the terminal shows:
        """
        passlet {version}
        """

    Scenario: Show pair help
      When I run `passlet pair --help`
      Then the terminal shows:
        """
        Pair this Agent with an Approver using the one-time command printed by `passlet approver pair`. Approve pairing on your Mac with Touch ID. The endpoint and trusted key are saved locally for future requests.

        Usage of pair:
          --code string
                single-use pairing code from your Mac
          --endpoint string
                Approver endpoint (defaults to the local socket)
        """

    Scenario: Show doctor help
      When I run `passlet doctor --help`
      Then the terminal shows:
        """
        Check your Approver connection and verify GitHub access. Requires approval on your Approver Mac.

        Usage of doctor:
          --endpoint string
                override the environment, saved endpoint, or default local socket
        """

    Scenario: Show exec help
      When I run `passlet exec --help`
      Then the terminal shows:
        """
        Run a command with an approved GitHub grant. Use --grant with a saved file or Bash/Zsh process substitution. Put the command after --.

        Example:
          passlet request --repos alice/repo --permissions contents:read --out grant.json
          passlet exec --grant grant.json -- gh repo view alice/repo

        Usage of exec:
          --grant string
                grant JSON file or process-substitution path (required)
        """

    Scenario: Show request help
      When I run `passlet request --help`
      Then the terminal shows:
        """
        Request scoped access from your Approver. After approval, print the grant as JSON or save it with --out for later use with `passlet exec`.

        Save and use a grant:
          passlet request --repos alice/repo --permissions contents:read --out grant.json
          passlet exec --grant grant.json -- gh repo view alice/repo

        One-time grant use directly with exec (Bash/Zsh):
          passlet exec --grant <(passlet request --repos alice/repo --permissions contents:read) -- gh repo view alice/repo

        Usage of request:
          --account string
                account-wide permission target (exclusive with --repos/--org)
          --org string
                organization-wide permission target (exclusive with --repos/--account)
          --out string
                write grant JSON to a new mode-0600 file
          --permissions string
                comma-separated permission:read|write|admin list
          --repos string
                comma-separated owner/repository list
        """

  Rule: Approver commands

    Scenario: Show audit help
      When I run `passlet approver audit --help`
      Then the terminal shows:
        """
        Show recent access events from the local audit log.

        Usage of approver audit:
          --json
                print structured records as JSON
          --limit int
                recent events, 1–1000 (default 20)
        """

    Scenario: Show doctor help
      When I run `passlet approver doctor --help`
      Then the terminal shows:
        """
        Check service health and credentials on this Mac. Inspects the shared local/SSH socket and Tailscale connection.

        Requires Touch ID approval to verify saved credentials.
        """

    Scenario: Show approver start help
      When I run `passlet approver start --help`
      Then the terminal shows:
        """
        Run Passlet Approver service on your Mac to receive access requests and ask for your approval. Homebrew and Home Manager normally start it automatically.
        """

  Rule: Approver GitHub commands

    Scenario: Show github reset help
      When I run `passlet approver github reset --help`
      Then the terminal shows:
        """
        Remove saved GitHub credentials from this Approver Mac. Your GitHub App and its installations remain on GitHub.
        """

    Scenario: Show github setup help
      When I run `passlet approver github setup --help`
      Then the terminal shows:
        """
        Set up GitHub access on your Approver Mac in your browser.

        Credentials are saved in the macOS Keychain.

        Run again to add another organization.

        Usage of approver github setup:
          --handle string
                GitHub handle used to pre-fill the App name (prompted if omitted)
        """

  Rule: Pairing management

    Scenario: Show Approver pairing help
      When I run `passlet approver pair --help`
      Then the terminal shows:
        """
        Create a pairing code for one Agent. The code expires after 10 minutes. Pairing requires Touch ID and does not access stored credentials.
        """

    Scenario: Show paired Agent management help
      When I run `passlet approver agents --help`
      Then the terminal shows:
        """
        List paired Agents or revoke access.

        Usage: passlet approver agents list
               passlet approver agents revoke <name>
        """

  Rule: SSH-agent commands

    Scenario: Show SSH-agent proxy help
      When I run `passlet ssh-agent --help`
      Then the terminal shows:
        """
        Provide a private SSH-agent socket for this machine. Requests are forwarded on demand to your paired Approver. Configure the upstream SSH-agent socket on your Approver Mac.

        Usage of ssh-agent:
        """
