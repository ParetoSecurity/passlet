Feature: Review local audit history
  Audit reads the Conductor's existing log without requesting approval or contacting GitHub.

  Scenario: Review recent events
    Given the audit log contains:
      | time                 | request   | outcome   |
      | 2026-11-03T12:00:00Z  | request-1 | requested |
      | 2026-11-03T12:01:00Z  | request-1 | approved  |
    When I run `passlet audit`
    Then the terminal shows:
      """
      2026-11-03T12:01:00Z  approved
        Request: request-1
        Agent: Claude
        Caller: cruncher (ssh)
        Repositories: alice/repo
        Requested permissions: contents: write, metadata: read

      2026-11-03T12:00:00Z  requested
        Request: request-1
        Agent: Claude
        Caller: cruncher (ssh)
        Repositories: alice/repo
        Requested permissions: contents: write, metadata: read
      """

  Scenario: Limit the number of events
    Given the audit log contains:
      | time                 | request   | outcome   |
      | 2026-11-03T12:00:00Z  | request-1 | requested |
      | 2026-11-03T12:01:00Z  | request-1 | approved  |
    When I run `passlet audit --limit 1`
    Then the terminal shows:
      """
      2026-11-03T12:01:00Z  approved
        Request: request-1
        Agent: Claude
        Caller: cruncher (ssh)
        Repositories: alice/repo
        Requested permissions: contents: write, metadata: read
      """

  Scenario: Read an empty audit log
    Given the audit log is empty
    When I run `passlet audit`
    Then the terminal shows:
      """
      No audit events.
      """

  Scenario: Export events as JSON
    Given the audit log contains:
      | time                 | request   | outcome   |
      | 2026-11-03T12:00:00Z  | request-1 | requested |
    When I run `passlet audit --json`
    Then the terminal shows:
      """
      [{"at":"2026-11-03T12:00:00Z","event":{"request_id":"request-1","caller":{"transport":"ssh","node_id":"","name":"cruncher"},"requested":{"agent":"Claude","repositories":["alice/repo"],"permissions":{"contents":"write","metadata":"read"}},"outcome":"requested","approver":"human"}}]
      """

  Scenario: Export an empty audit log as JSON
    Given the audit log is empty
    When I run `passlet audit --json`
    Then the terminal shows:
      """
      []
      """

  Scenario: Read audit history before a database exists
    Given no audit database exists
    When I run `passlet audit`
    Then the terminal shows:
      """
      passlet: audit_unavailable
      """
    And the command exits unsuccessfully
    And no audit database is created

  Scenario: Skip corrupted records with a warning
    Given the audit log contains:
      | time                 | request   | outcome   |
      | 2026-11-03T12:00:00Z  | request-1 | requested |
    And the audit log contains a corrupted record
    When I run `passlet audit`
    Then the terminal shows:
      """
      Warning: audit log contains corrupted entries (1 skipped).
      2026-11-03T12:00:00Z  requested
        Request: request-1
        Agent: Claude
        Caller: cruncher (ssh)
        Repositories: alice/repo
        Requested permissions: contents: write, metadata: read
      """
    And the warning is written to standard error

  Scenario: Keep JSON output valid when skipping corrupted records
    Given the audit log contains:
      | time                 | request   | outcome   |
      | 2026-11-03T12:00:00Z  | request-1 | requested |
    And the audit log contains a corrupted record
    When I run `passlet audit --json`
    Then the terminal shows:
      """
      Warning: audit log contains corrupted entries (1 skipped).
      [{"at":"2026-11-03T12:00:00Z","event":{"request_id":"request-1","caller":{"transport":"ssh","node_id":"","name":"cruncher"},"requested":{"agent":"Claude","repositories":["alice/repo"],"permissions":{"contents":"write","metadata":"read"}},"outcome":"requested","approver":"human"}}]
      """
    And the warning is written to standard error
    And standard output is valid JSON
