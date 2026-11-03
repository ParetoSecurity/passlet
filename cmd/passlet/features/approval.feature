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
      • contents: read
      • metadata: read.
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
      • contents: read
      • metadata: read.
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
      • contents: read
      • metadata: read.
      """
