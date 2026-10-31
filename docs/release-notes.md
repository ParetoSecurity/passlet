Passlet gives agents repository- and permission-scoped GitHub user tokens after a native approval on your Mac. GitHub actions retain your attribution, and agents can keep using delivered tokens while your Mac is offline.

Homebrew and Nix/Home Manager own installation, updates and per-user login startup. Use the CLI for GitHub onboarding, requests, diagnostics and audit history. The signed macOS helper handles protected Keychain access and native approval, with no menu app or self-updater.

Local agents use a private Unix socket. Remote agents connect through per-user SSH socket forwarding or explicitly enabled Tailscale HTTPS. Each person configures their own GitHub App and account.

macOS requires version 14 or later and a logged-in graphical session. Linux agents are supported on amd64 and arm64. Follow docs/INSTALLATION.md after the release's package metadata has been published.
