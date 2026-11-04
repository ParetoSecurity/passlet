Feature: Show command help

  Scenario: Explicit help does not inspect service configuration
    When I run `passlet --help`
    Then the terminal shows:
      """
      Give your remote agents scoped access to services, approved on-the-fly from your laptop. Run passlet without arguments to see how this machine is configured.

      Usage: passlet <command>

      Commands:
        connect       Check and save a Conductor endpoint
        github setup  Configure GitHub access on your Conductor Mac
        github reset  Remove saved GitHub credentials
        conductor     Run the Conductor
        request       Request scoped access
        exec          Run a command with scoped access
        doctor        Verify Conductor and GitHub access with approval
        audit         Show local audit history

      Run passlet <command> --help for command options.
      """

  Scenario: Show connect help
    When I run `passlet connect --help`
    Then the terminal shows:
      """
      Tell Passlet Agent how to connect to Passlet Conductor. Run once on each Agent machine. The connection is saved in ~/.config/passlet/endpoint for future commands.

      Usage: passlet connect <https://host:port or unix:///absolute/socket>
      """

  Scenario: Show github setup help
    When I run `passlet github setup --help`
    Then the terminal shows:
      """
      Set up GitHub access on your Conductor Mac in your browser. Credentials are saved in the macOS Keychain. Run again to add another organization.

      Usage of github setup:
        --handle string
              GitHub handle used to pre-fill the App name (prompted if omitted)
      """

  Scenario: Show github reset help
    When I run `passlet github reset --help`
    Then the terminal shows:
      """
      Remove saved GitHub credentials from this Conductor Mac. Your GitHub App and its installations remain on GitHub.
      """

  Scenario: Show conductor help
    When I run `passlet conductor --help`
    Then the terminal shows:
      """
      Run Passlet Conductor service on your Mac to receive access requests and ask for your approval. Homebrew and Home Manager normally start it automatically.
      """

  Scenario: Show request help
    When I run `passlet request --help`
    Then the terminal shows:
      """
      Request scoped access from your Conductor. After approval, print the grant as JSON or save it with --out for later use with passlet exec.

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

  Scenario: Show exec help
    When I run `passlet exec --help`
    Then the terminal shows:
      """
      Run a command with an approved GitHub grant. Use --grant with a saved file or Bash/Zsh process substitution. Put the command after --.

      Usage of exec:
        --grant string
              grant JSON file or process-substitution path (required)
      """

  Scenario: Show doctor help
    When I run `passlet doctor --help`
    Then the terminal shows:
      """
      Check your Conductor connection and verify GitHub access. Requires approval on your Conductor Mac. No agent token is issued.

      Usage of doctor:
        --endpoint string
              override the environment, saved endpoint, or default local socket
      """

  Scenario: Show audit help
    When I run `passlet audit --help`
    Then the terminal shows:
      """
      Show recent access events from the local audit log. Run on your Conductor Mac to review requests and approval decisions.

      Usage of audit:
        --json
              print structured records as JSON
        --limit int
              recent events, 1–1000 (default 20)
      """
