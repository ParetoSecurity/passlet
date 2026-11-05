Feature: Remember an Approver endpoint
  Connecting checks reachability without approval or credential access.
  Failed connections preserve the previously saved endpoint.

  Scenario: Connect to an Approver
    Given a reachable Approver
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      Connected to Approver: {endpoint}
      """
    And the endpoint is saved for future commands

  Scenario: Replace a saved connection
    Given a reachable Approver
    And the saved endpoint is "https://previous.example.ts.net:17400"
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      Connected to Approver: {endpoint}
      """
    And the endpoint is saved for future commands

  Scenario: An unavailable Approver preserves the saved connection
    Given a reachable Approver
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And that Approver stops
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: Could not connect to Approver: {endpoint}
      Your previous Approver connection has been kept.

      Run `passlet doctor --endpoint {endpoint}` for more information.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged

  Scenario: An unrelated service preserves the saved connection
    Given a reachable Approver
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And the service does not return Passlet health information
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: Could not connect to Approver: {endpoint}
      Your previous Approver connection has been kept.

      Run `passlet doctor --endpoint {endpoint}` for more information.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged

  Scenario: Refuse to connect while an environment endpoint is set
    Given a reachable Approver
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And PASSLET_ENDPOINT selects another Approver
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: PASSLET_ENDPOINT already selects an Approver.
      Unset PASSLET_ENDPOINT before running `passlet connect`.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged
    And requests still use the environment endpoint
