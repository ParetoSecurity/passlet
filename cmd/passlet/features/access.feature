Feature: Request and use scoped access
  An Agent receives only the approved grant. Commands can use it without contacting the Approver again.

  Background:
    Given an Approver with GitHub access for alice

  Scenario: Save and use an approved grant
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    Then a private grant is saved for alice/repo with contents:read
    When I run `passlet exec --grant {grant} -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Reuse a grant while the Approver is offline
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    And the Approver goes offline
    And I run `passlet exec --grant {grant} -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Pass a one-off grant without consuming the command's input
    Given I approve the access request
    And the command's standard input is "input for the child"
    When I run `passlet exec --grant <(passlet request --no-cache --stdout --repos alice/repo --permissions contents:read) -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      input for the child
      """
    And no grant file is saved
    And no grant cache is created

  Scenario: Denied approval does not start the command
    Given I deny the access request
    When I run `passlet exec --grant <(passlet request --no-cache --stdout --repos alice/repo --permissions contents:read) -- check-access`
    Then the command fails without starting check-access
    And no grant file is saved
    And the error includes `approval_denied`
    And GitHub is not contacted

  Scenario: Denied approval does not save a grant
    Given I deny the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    Then the command fails without starting check-access
    And no grant file is saved
    And the error includes `approval_denied`
    And GitHub is not contacted

  Scenario: An expired grant does not start the command
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    And the saved grant expires
    And I run `passlet exec --grant {grant} -- check-access`
    Then the command fails without starting check-access
    And the error includes `grant_expired_or_invalid`

  Scenario: A new session reuses cached access while the Approver is offline
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read`
    And the Approver goes offline
    And I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Bypassing the cache requires approval even when a grant is cached
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read`
    Given I deny the access request
    When I run `passlet exec --grant <(passlet request --no-cache --stdout --repos alice/repo --permissions contents:read) -- check-access`
    Then the command fails without starting check-access
    And the error includes `approval_denied`
    When I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Bypassing the cache does not store the approved grant
    Given I approve the access request
    When I run `passlet exec --grant <(passlet request --no-cache --stdout --repos alice/repo --permissions contents:read) -- true`
    Then the command succeeds
    And no grant file is saved
    And no grant cache is created
    When the Approver goes offline
    And I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the command fails without starting check-access
    And the error includes `approver_unavailable_or_connection_lost`

  Scenario: Default requests keep grant JSON out of terminal output
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read`
    Then the command succeeds
    And no grant file is saved
    When I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Scoped execution requests approval and reuses it while offline
    Given I approve the access request
    When I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """
    When the Approver goes offline
    And I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Denied scoped execution does not start the command
    Given I deny the access request
    When I run `passlet exec --repos alice/repo --permissions contents:read -- check-access`
    Then the command fails without starting check-access
    And the error includes `approval_denied`
    And GitHub is not contacted

  Scenario: Scoped execution without a command does not request approval
    Given I approve the access request
    When I run `passlet exec --repos alice/repo --permissions contents:read`
    Then the command fails without starting check-access
    And the error includes `exec requires`
    And GitHub is not contacted
    And no grant cache is created
