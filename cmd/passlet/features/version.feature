Feature: Show the installed version

  Scenario: Version does not inspect service configuration
    When I run `passlet --version`
    Then the terminal shows:
      """
      passlet {version}
      """
    And no local service or Tailscale configuration is queried
