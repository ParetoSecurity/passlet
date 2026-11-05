Feature: Request and use scoped access
  An Agent receives only the approved grant. Commands can use it without contacting the Conductor again.

  Background:
    Given a Conductor with GitHub access for alice

  Scenario: Save and use an approved grant
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    Then a private grant is saved for alice/repo with contents:read
    When I run `passlet exec --grant {grant} -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Reuse a grant while the Conductor is offline
    Given I approve the access request
    When I run `passlet request --repos alice/repo --permissions contents:read --out {grant}`
    And the Conductor goes offline
    And I run `passlet exec --grant {grant} -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      """

  Scenario: Pass a one-off grant without consuming the command's input
    Given I approve the access request
    And the command's standard input is "input for the child"
    When I run `passlet exec --grant <(passlet request --repos alice/repo --permissions contents:read) -- check-access`
    Then the terminal shows:
      """
      Scoped credentials received
      input for the child
      """
    And no grant file is saved

  Scenario: Denied approval does not start the command
    Given I deny the access request
    When I run `passlet exec --grant <(passlet request --repos alice/repo --permissions contents:read) -- check-access`
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
