Feature: Understand how Passlet is configured
  Running Passlet without arguments shows its role, reachability, and addresses with a hint for finding command help.
  This overview never requests approval, unlocks credentials, or issues a pass.

  Scenario: Run Passlet for the first time
    Given Passlet is installed and configured as Conductor
    When I run `passlet`
    Then the terminal shows:
      """
      Configured as a Conductor. Agents send requests here for your approval.

      Local agents on this Mac connect automatically.

      See `passlet --help` for available commands.
      """

  Scenario: Conductor with verified Tailscale forwarding
    Given the Conductor is configured for Tailscale
    When I run `passlet`
    Then the terminal shows:
      """
      Configured as a Conductor. Agents send requests here for your approval.

      Local agents on this Mac connect automatically.

      Tell remote agents how to connect:
        passlet connect https://alice-mac.example.ts.net:17400

      See `passlet --help` for available commands.
      """

  Scenario: Run Agent for the first time
    Given Passlet is installed
    When I run `passlet`
    Then the terminal shows:
      """
      Configured as an Agent. Requests are sent to another Passlet running in Conductor mode for approval.

      WARNING! No local Conductor found.

      Run `passlet` on your Conductor machine to see how to connect.
      """

  Scenario: Run Agent after connecting to a Conductor
    Given I have run `passlet connect https://alice-mac.example.ts.net:17400`
    When I run `passlet`
    Then the terminal shows:
      """
      Configured as an Agent. Requests are sent to another Passlet running in Conductor mode for approval.

      Sending requests to: https://alice-mac.example.ts.net:17400

      See `passlet --help` for available commands.
      """

  Scenario: An Agent cannot reach its Conductor
    Given Passlet is installed
    And PASSLET_ENDPOINT is "https://alice-mac.example.ts.net:17400"
    And the selected Conductor is unreachable
    When I run `passlet`
    Then the terminal shows:
      """
      Configured as an Agent. Requests are sent to another Passlet running in Conductor mode for approval.

      WARNING! The Passlet Conductor is not reachable.
      Run `passlet doctor` for more information.
      """
