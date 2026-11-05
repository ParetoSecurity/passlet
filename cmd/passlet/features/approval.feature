Feature: Review a request in the Touch ID pane
  The approval text identifies the caller and the exact access being requested.

  Scenario: Review a request from a local Agent
    Given an Agent on this Mac
    And the request is made from Claude Code
    When I run `passlet request --repos alice/repo --permissions contents:read`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Claude from local
      • 8 hours

      Repositories:
      • alice/repo

      Permissions:
      • contents: read.
      """

  Scenario: Review a request through an SSH tunnel
    Given an Agent connected through the "cruncher" SSH tunnel
    And the request is made from Codex
    When I run `passlet request --repos alice/repo --permissions contents:read`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Codex from cruncher
      • 8 hours

      Repositories:
      • alice/repo

      Permissions:
      • contents: read.
      """

  Scenario: Review a request through Tailscale
    Given an Agent connected through Tailscale as "cruncher.example.ts.net."
    When I run `passlet request --repos alice/repo --permissions contents:read`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Shell from cruncher
      • 8 hours

      Repositories:
      • alice/repo

      Permissions:
      • contents: read.
      """

  Scenario: Request access to manage GitHub account SSH keys
    Given an Approver configured for GitHub account `alice`
    And the request is made from Claude Code
    When I run `passlet request --account alice --permissions git_ssh_keys:write`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Claude from local
      • 8 hours

      Account: alice

      Repositories:
      • all

      Permissions:
      • git_ssh_keys: write.
      """

  Scenario: Review organization-wide access
    Given an Agent on this Mac
    And the request is made from Claude Code
    When I run `passlet request --org acme --permissions members:write`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Claude from local
      • 8 hours

      Organization: acme

      Repositories:
      • all

      Permissions:
      • members: write.
      """

  Scenario: Summarize a long repository list without hiding permissions
    Given an Agent on this Mac
    And the request is made from Claude Code
    And repositories `alice/repository-001` through `alice/repository-100`
    When I run `passlet request --repos {repositories} --permissions contents:read`
    Then the Touch ID pane shows:
      """
      Passlet is trying to grant access to GitHub for:
      • Claude from local
      • 8 hours

      Repositories:
      • alice/repository-001
      • alice/repository-002
      • alice/repository-003
      • alice/repository-004
      • alice/repository-005
      • alice/repository-006
      • alice/repository-007
      • alice/repository-008
      • alice/repository-009
      • alice/repository-010
      • alice/repository-011
      • alice/repository-012
      • alice/repository-013
      • alice/repository-014
      • alice/repository-015
      • alice/repository-016
      • alice/repository-017
      • alice/repository-018
      • alice/repository-019
      • alice/repository-020
      and 80 additional repositories

      Permissions:
      • contents: read.
      """

  Scenario: Review a diagnostic check
    Given an Agent connected through the "cruncher" SSH tunnel
    When I run `passlet doctor`
    Then the Touch ID pane shows:
      """
      Passlet is trying to verify stored credentials.
      """
