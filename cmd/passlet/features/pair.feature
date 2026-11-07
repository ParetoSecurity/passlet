Feature: Pair an Agent with an Approver
  Pairing establishes mutual trust without accessing provider credentials.

  Scenario: Print pairing instructions for the default socket
    Given the Approver has Tailscale disabled
    When I run `passlet approver pair`
    Then the terminal shows:
      """
      For an Agent using the default socket (local or SSH-forwarded), run:
        passlet pair --code {code}

      For a different SSH-forwarded socket, use `--endpoint unix:///path/to/socket`.
      The code expires in 10 minutes and works once. Approve pairing on this Mac with Touch ID.
      """

  Scenario: Print pairing instructions with verified Tailscale forwarding
    Given the Approver has verified Tailscale forwarding
    When I run `passlet approver pair`
    Then the terminal shows:
      """
      For an Agent using the default socket (local or SSH-forwarded), run:
        passlet pair --code {code}

      For an Agent connecting through Tailscale, run:
        passlet pair --endpoint https://mac.example.ts.net:17400 --code {code}

      For a different SSH-forwarded socket, use `--endpoint unix:///path/to/socket`.
      The code expires in 10 minutes and works once. Approve pairing on this Mac with Touch ID.
      """

  Scenario Outline: Explain why Tailscale pairing instructions are unavailable
    Given the Approver has Tailscale enabled but its CLI is <state>
    When I run `passlet approver pair`
    Then the terminal shows:
      """
      For an Agent using the default socket (local or SSH-forwarded), run:
        passlet pair --code {code}

      WARNING: Tailscale pairing instructions are unavailable.
      <reason>

      For a different SSH-forwarded socket, use `--endpoint unix:///path/to/socket`.
      The code expires in 10 minutes and works once. Approve pairing on this Mac with Touch ID.
      """

    Examples:
      | state   | reason                                                                                         |
      | missing | Can't find `tailscale` command. Please add it to your PATH.                                      |
      | failing | Tailscale status could not be read or did not include a running node with a device name.         |

  Scenario: Pair an Agent for the first time
    Given an Approver with a fresh pairing code
    When I run `passlet pair --endpoint {endpoint} --code {code}`
    Then the terminal shows:
      """
      Approve pairing on your Mac with Touch ID.
      Paired with Approver: {endpoint}
      """
    And the Touch ID dialog shows:
      """
      Passlet is trying to pair Agent {agent}.

      Only approve if you started this pairing.
      """
    And the Agent can make authenticated requests

  Scenario: Decline pairing
    Given an Approver with a fresh pairing code
    And pairing approval is denied
    When I run `passlet pair --endpoint {endpoint} --code {code}`
    Then the terminal shows:
      """
      Approve pairing on your Mac with Touch ID.
      error: pairing failed or was denied; create a new code with `passlet approver pair`
      """
    And no connection is saved

  Scenario: A pairing code can only be used once
    Given an Approver with a fresh pairing code
    And the code has already been used
    When I run `passlet pair --endpoint {endpoint} --code {code}`
    Then the terminal shows:
      """
      Approve pairing on your Mac with Touch ID.
      error: pairing failed or was denied; create a new code with `passlet approver pair`
      """
    And the existing connection still works

  Scenario: Reject a positional endpoint
    Given an Approver with a fresh pairing code
    When I run `passlet pair {endpoint} --code {code}`
    Then the terminal shows:
      """
      error: usage: passlet pair [--endpoint <endpoint>] --code <code>
      """
    And no connection is saved
