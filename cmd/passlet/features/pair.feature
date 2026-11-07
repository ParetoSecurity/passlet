Feature: Pair an Agent with an Approver
  Pairing establishes mutual trust without accessing provider credentials.

  Scenario: Pair an Agent for the first time
    Given an Approver with a fresh pairing code
    When I run `passlet pair {endpoint} --code {code}`
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
    When I run `passlet pair {endpoint} --code {code}`
    Then the terminal shows:
      """
      Approve pairing on your Mac with Touch ID.
      error: pairing failed or was denied; create a new code with `passlet approver pair`
      """
    And no connection is saved

  Scenario: A pairing code can only be used once
    Given an Approver with a fresh pairing code
    And the code has already been used
    When I run `passlet pair {endpoint} --code {code}`
    Then the terminal shows:
      """
      Approve pairing on your Mac with Touch ID.
      error: pairing failed or was denied; create a new code with `passlet approver pair`
      """
    And the existing connection still works
