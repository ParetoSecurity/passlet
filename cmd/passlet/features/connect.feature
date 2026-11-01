Feature: Remember a Conductor endpoint
  Connecting checks reachability without approval or credential access.
  Failed connections preserve the previously saved endpoint.

  Scenario: Connect to a Conductor
    Given a reachable Conductor
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      Connected to Conductor: {endpoint}
      """
    And the endpoint is saved for future commands

  Scenario: Replace a saved connection
    Given a reachable Conductor
    And the saved endpoint is "https://previous.example.ts.net:17400"
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      Connected to Conductor: {endpoint}
      """
    And the endpoint is saved for future commands

  Scenario: An unavailable Conductor preserves the saved connection
    Given a reachable Conductor
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And that Conductor stops
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: Could not connect to Conductor: {endpoint}
      Your previous Conductor connection has been kept.

      Run `passlet doctor --endpoint {endpoint}` for more information.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged

  Scenario: An unrelated service preserves the saved connection
    Given a reachable Conductor
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And the service does not return Passlet health information
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: Could not connect to Conductor: {endpoint}
      Your previous Conductor connection has been kept.

      Run `passlet doctor --endpoint {endpoint}` for more information.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged

  Scenario: Refuse to connect while an environment endpoint is set
    Given a reachable Conductor
    And the saved endpoint is "https://previous.example.ts.net:17400"
    And PASSLET_ENDPOINT selects another Conductor
    When I run `passlet connect {endpoint}`
    Then the terminal shows:
      """
      WARNING: PASSLET_ENDPOINT already selects a Conductor.
      Unset PASSLET_ENDPOINT before running passlet connect.
      """
    And the command exits unsuccessfully
    And the saved endpoint remains unchanged
    And requests still use the environment endpoint
