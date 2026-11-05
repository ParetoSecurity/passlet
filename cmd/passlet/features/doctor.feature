Feature: Diagnose Approver access
  Doctor verifies credentials with approval, without issuing an Agent token.

  Rule: Passlet commands

    Scenario: Verify GitHub access
      Given an Approver with valid GitHub credentials
      And I approve the diagnostic check
      When I run `passlet doctor`
      Then the terminal shows:
        """
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
        Endpoint: {endpoint}
        error: No forwarded socket found. Your SSH tunnel may not be running.
        """
      And the command fails

    Scenario: Deny the diagnostic check
      Given an Approver with valid GitHub credentials
      And I deny the diagnostic check
      When I run `passlet doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: Approval was denied.
        """
      And the command fails

    Scenario: GitHub has not been set up
      Given an Approver without GitHub credentials
      When I run `passlet doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: GitHub credentials are not configured.
        Run `passlet approver github setup` on your Mac.
        """
      And the command fails

    Scenario: A stale forwarded socket refuses connections
      Given the forwarded socket is stale
      When I run `passlet doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        error: The forwarded socket is not accepting connections. Restart your SSH tunnel.
        """
      And the command fails

    Scenario: The forwarded socket does not reach a healthy Approver
      Given the forwarded socket returns no healthy Approver response
      When I run `passlet doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        error: The socket did not return a healthy Approver response. Check that Passlet Approver is running on your Mac and that the SSH tunnel forwards to its socket.
        """
      And the command fails

  Rule: Approver commands

    Scenario: The local Approver service is stopped
      Given I am checking the local Approver
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        error: local Approver service is not running
        """
      And the command fails

    Scenario: A stale local socket refuses connections
      Given I am checking the local Approver
      And the local socket is stale
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        error: local Approver service is not running
        """
      And the command fails

    Scenario: The local socket returns an unhealthy response
      Given I am checking the local Approver
      And the local socket returns no healthy Approver response
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        error: The local socket did not return a healthy Approver response.
        """
      And the command fails

    Scenario: Deny local diagnostics
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And I deny the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        error: Approval was denied.
        """
      And the command fails

    Scenario: Local diagnostics ignore the selected remote endpoint
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          SSH sockets: disabled
          Tailscale: disabled

        Enable SSH sockets: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels
        Enable Tailscale: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        """

    Scenario: SSH and Tailscale listeners are ready
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has listening SSH and Tailscale sockets
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
        ✓ SSH socket (lima): listening
        ✓ SSH socket (orbstack): listening
        ✓ Tailscale: listening

        On Tailscale agents, run this once:
          passlet connect https://alice-mac.example.ts.net:17400
        """

    Scenario: A failed SSH listener does not hide working connections
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has listening SSH and Tailscale sockets
      And the SSH listener for "lima" stops
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          SSH socket (lima): unavailable
        ✓ SSH socket (orbstack): listening
        ✓ Tailscale: listening

        Socket file is missing: {state_dir}/ssh-lima.sock

        SSH troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels

        On Tailscale agents, run this once:
          passlet connect https://alice-mac.example.ts.net:17400
        error: one or more configured listeners are unavailable
        """
      And the command fails

    Scenario: Configured SSH and Tailscale listeners are unavailable
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has unavailable SSH and Tailscale sockets
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          SSH socket (lima): unavailable
          SSH socket (orbstack): unavailable
          Tailscale: unavailable

        Socket file is missing: {state_dir}/ssh-lima.sock

        Socket file is missing: {state_dir}/ssh-orbstack.sock

        Socket file is missing: {state_dir}/passlet.sock.tailscale

        SSH troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels

        Tailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        error: one or more configured listeners are unavailable
        """
      And the command fails

    Scenario Outline: Explain unavailable Tailscale forwarding
      Given I am checking the local Approver
      And an Approver with valid GitHub credentials
      And the Approver has a listening Tailscale socket
      And Tailscale <condition>
      And I approve the diagnostic check
      When I run `passlet approver doctor`
      Then the terminal shows:
        """
        Endpoint: {endpoint}
        Approver: running
        Approve the diagnostic check on your Mac with Touch ID.
        Keychain: unlocked
        GitHub authorization: valid
        GitHub identity: alice

        ✓ Local socket: listening
          SSH sockets: disabled
          Tailscale: unavailable

        Enable SSH sockets: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#ssh-tunnel-labels

        <explanation>

        Tailscale troubleshooting: https://github.com/ParetoSecurity/passlet/blob/main/docs/SETUP.md#tailscale
        error: one or more configured listeners are unavailable
        """
      And the command fails

      Examples:
        | condition                      | explanation                                                                                          |
        | is not running                 | Tailscale is not running (state: Stopped).                                                           |
        | has no forwarding configured   | No Tailscale Serve forwarding is configured on port 17400.                                           |
        | forwards to a different socket | Tailscale Serve forwards to unix:/tmp/other.sock instead of unix:{state_dir}/passlet.sock.tailscale. |
