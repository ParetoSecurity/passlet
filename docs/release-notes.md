Passlet gives remote agents repository- and permission-scoped GitHub user tokens
after a native approval on your Mac. GitHub actions retain your attribution, and
agents can keep using delivered tokens while your Mac is offline.

This first release includes the Go CLI/service, native macOS Keychain helper,
Tailscale caller identification, structured SQLite audit records, and a Claude
skill. GitHub App creation and authorization are manual in v0.1.

macOS requires macOS 14 or later, Tailscale, and a logged-in graphical session.
Linux CLI archives are available for amd64 and arm64. Review the setup and trust
boundaries in README.md before configuring the service.
