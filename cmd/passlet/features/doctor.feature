Feature: Diagnose Conductor access
  Doctor verifies credentials with approval, without issuing an Agent token.

  Scenario: Verify GitHub access
    Given a Conductor with valid GitHub credentials
    And I approve the diagnostic check
    When I run `passlet doctor`
    Then the terminal shows:
      """
      Endpoint: {endpoint}
      Conductor: running
      Approve the diagnostic check on your Mac with Touch ID or your password.
      Keychain: unlocked
      GitHub authorization: valid
      GitHub identity: alice
      No agent token was issued.
      """

  Scenario: The local Conductor service is stopped
    Given the local Conductor service is stopped
    When I run `passlet doctor`
    Then the terminal shows:
      """
      Endpoint: {endpoint}
      passlet: local Conductor service is not running
      """
    And the command fails

  Scenario: The Conductor is unreachable
    Given the selected Conductor is unreachable
    When I run `passlet doctor`
    Then the terminal shows:
      """
      Endpoint: {endpoint}
      Conductor: unavailable. Check the service on your Mac and your SSH tunnel or Tailscale endpoint.
      passlet: doctor_found_problems
      """
    And the command fails

  Scenario: Deny the diagnostic check
    Given a Conductor with valid GitHub credentials
    And I deny the diagnostic check
    When I run `passlet doctor`
    Then the terminal shows:
      """
      Endpoint: {endpoint}
      Conductor: running
      Approve the diagnostic check on your Mac with Touch ID or your password.
      passlet: approval_denied
      """
    And the command fails

  Scenario: GitHub has not been set up
    Given a Conductor without GitHub credentials
    When I run `passlet doctor`
    Then the terminal shows:
      """
      Endpoint: {endpoint}
      Conductor: running
      Approve the diagnostic check on your Mac with Touch ID or your password.
      GitHub credentials: verification failed (credentials_missing).
      Run passlet github setup on your Mac.
      passlet: credentials_missing
      """
    And the command fails
