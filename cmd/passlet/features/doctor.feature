Feature: Diagnose Approver access
  Doctor verifies credentials with approval, without issuing an Agent token.

  Rule: Passlet commands

    Scenario Outline: Explain pairing failures before requesting approval
      Given an Approver with valid GitHub credentials
      And the Agent pairing <problem>
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: <message>

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

      Examples:
        | problem                  | message                                                                                                                   |
        | configuration is missing | Agent pairing is not configured. Run `passlet approver pair` on your Mac, then use its pairing command on this Agent.                                |
        | pin is incorrect         | The Approver public key does not match your pairing configuration. Verify its identity before changing the trusted key.   |
        | key is not trusted       | The Approver rejected pairing authentication. Check that it trusts this Agent's public key.                               |
        | key has not been generated | The Approver rejected pairing authentication. Check that it trusts this Agent's public key.                               |

    Scenario: Verify GitHub access
      Given an Approver with valid GitHub credentials
      And I approve the diagnostic check
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice
        """

    Scenario: The forwarded socket is missing
      Given the selected Approver is unreachable
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: No forwarded socket found. Your SSH tunnel may not be running.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: Deny the diagnostic check
      Given an Approver with valid GitHub credentials
      And I deny the diagnostic check
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: Approval was denied.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: GitHub has not been set up
      Given an Approver without GitHub credentials
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: GitHub credentials are not configured.
        Run `passlet approver github setup` on your Mac.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: A stale forwarded socket refuses connections
      Given the forwarded socket is stale
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: The forwarded socket is not accepting connections. Restart your SSH tunnel.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: The forwarded socket does not reach a healthy Approver
      Given the forwarded socket returns no healthy Approver response
      When I run `passlet doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: The socket did not return a healthy Approver response. Check that Passlet Approver is running on your Mac and that the SSH tunnel forwards to its socket.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

  Rule: Approver commands

    Scenario: The local Approver service is stopped
      Given I am checking the local Approver
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: local Approver service is not running

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: A stale local socket refuses connections
      Given I am checking the local Approver
      And the local socket is stale
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: local Approver service is not running

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: The local socket returns an unhealthy response
      Given I am checking the local Approver
      And the local socket returns no healthy Approver response
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        error: The local socket did not return a healthy Approver response.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: Deny local diagnostics
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And I deny the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: Approval was denied.

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario: Local diagnostics ignore the selected remote endpoint
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          Tailscale: disabled

        Enable Tailscale: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        """

    Scenario: Local and Tailscale listeners are ready
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has a listening Tailscale listener
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
        ✓ Tailscale: listening

        On Tailscale agents, run this once:
          passlet pair --endpoint https://alice-mac.example.ts.net:17400 --code <code>
        """

    Scenario: Configured Tailscale listener is unavailable
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has an unavailable Tailscale listener
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          Tailscale: unavailable

        Tailscale loopback listener is unavailable: tcp://127.0.0.1:17401

        Tailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        error: one or more configured listeners are unavailable

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

    Scenario Outline: Explain unavailable Tailscale forwarding
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has a listening Tailscale listener
      And Tailscale <condition>
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        passlet {version}
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          Tailscale: unavailable

        <explanation>

        Tailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        error: one or more configured listeners are unavailable

        Report issues at https://github.com/ParetoSecurity/passlet/issues/
        """
      And the command fails

      Examples:
        | condition                      | explanation                                                                                          |
        | is not installed | Can't find `tailscale` command. Please add it to your PATH. |
        | is not running                 | Tailscale is not running (state: Stopped).                                                           |
        | has no forwarding configured   | No Tailscale Serve forwarding is configured on port 17400.                                           |
        | forwards to a different backend | Tailscale Serve forwards to 127.0.0.1:9999 instead of 127.0.0.1:17401. |
