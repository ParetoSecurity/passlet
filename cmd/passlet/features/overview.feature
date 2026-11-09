Feature: Get started with Passlet
  Running Passlet shows the available commands.

  Scenario: Run Passlet for the first time
    When I run `passlet`
    Then the terminal shows:
      """
      Give your remote agents scoped access to services, approved on-the-fly from your Mac.

      Run `passlet approver` on your Mac to manage approvals and credentials.
      Your Agents then run `passlet request`.

      Usage: passlet <command>

      Commands:
        approver      Manage the local Approver on your Mac
        doctor        Check your connection and verify GitHub access
        exec          Run a command with scoped access
        pair          Pair with an Approver
        request       Request scoped access
        ssh-agent     Forward your Mac's SSH agent on demand

      Run `passlet <command> --help` for command options.
      """

  Scenario: Run Passlet Approver for the first time
    When I run `passlet approver`
    Then the terminal shows:
      """
      An approval service you run on your Mac.

      Agents request credentials from it, you approve them with Touch ID.

      Usage: passlet approver <command>

      Commands:
        agents        List or revoke paired Agents
        audit         Show audit history
        doctor        Check service health and credentials
        github        Manage GitHub credentials
        pair          Create a one-time pairing code
        start         Run the Approver service

      Run `passlet approver <command> --help` for command options.
      """

  Scenario: Run Passlet Approver GitHub for the first time
    When I run `passlet approver github`
    Then the terminal shows:
      """
      Manage GitHub credentials on your Approver Mac.

      Usage: passlet approver github <command>

      Commands:
        reset         Remove saved GitHub credentials
        setup         Configure GitHub access

      Run `passlet approver github <command> --help` for command options.
      """
