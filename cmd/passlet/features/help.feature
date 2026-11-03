Feature: Show command help

  Scenario: Explicit help does not inspect service configuration
    When I run `passlet --help`
    Then the terminal shows:
      """
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
        import        Import credentials using the signed helper
        --version     Show the installed version

      Run passlet <command> --help for command options.
      """
    And no local service or Tailscale configuration is queried
