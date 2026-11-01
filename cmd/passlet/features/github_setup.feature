Feature: Set up a personal GitHub App
  Credentials and app identity belong together in the protected Mac Keychain.

  Scenario: Set up GitHub for the first time
    Given Keychain contains no Passlet credentials
    And Git's `github.user` setting is `alice`
    When I run `passlet github setup`
    Then the terminal prompts "Please enter your GitHub handle:"
    And the input is prefilled with `alice`
    When I press Enter
    Then the terminal shows:
      """
      Passlet creates short-lived scoped tokens for your agents by acting as a GitHub App. Let's create one for you!

      This GitHub App is yours alone. Its credentials are stored in your Mac’s Keychain, protected by Touch ID, and used only to authenticate with GitHub. They are never sent to Passlet’s servers.

      Agents get only the short-lived, scoped access you approve with Touch ID—never your app’s full credentials.

      Passlet will start a temporary listener on localhost to receive GitHub's callbacks and verify your app setup. The listener runs only while `passlet github setup` is running.

      Press Enter to create your personal GitHub App in your browser.
      """
    And no browser opens
    When I press Enter
    Then my browser opens "http://127.0.0.1:45821/{session}/start"
    And the terminal shows:
      """
      If your browser didn't open, open this URL:
      http://127.0.0.1:45821/{session}/start
      """
    And the page automatically submits the app configuration to "https://github.com/settings/apps/new"
    And the app configuration sets the Setup URL to "http://127.0.0.1:45821/github/installed"
    When I confirm creating `passlet-alice` on GitHub
    And I authorize `passlet-alice` to act as `alice` on GitHub
    Then the same browser tab redirects to "https://github.com/apps/passlet-alice/installations/new"
    And the terminal shows:
      """
      Follow the instructions in your browser to install your app for an organization.

      Select "All repositories" so your agents can request access to any repository in that organization.

      Waiting for installation…
      """
    When I install `passlet-alice` for `ACME` with access to all repositories
    Then GitHub redirects my browser to "http://127.0.0.1:45821/github/installed" with the installation details
    And Passlet verifies the installation with GitHub before saving credentials
    And the terminal shows:
      """
      Verified: alice can access repositories in ACME through passlet-alice.
      """
    And credentials and verified identity are saved together in Keychain
    And the browser shows:
      """
      GitHub setup complete.
      You can close this tab and return to your terminal.
      """
    And the terminal shows:
      """
      GitHub setup complete.

      App: passlet-alice
      Account: alice

      Run `passlet github setup` again to add another organization.
      """

  Scenario: GitHub authorization is denied
    Given Keychain contains no Passlet credentials
    And I have created `passlet-alice` during GitHub setup for `alice`
    When I deny authorization of `passlet-alice` on GitHub
    Then the terminal shows:
      """
      GitHub authorization was denied. No credentials were saved.

      Your GitHub App still exists at https://github.com/settings/apps/passlet-alice/advanced.
      To start over, click "Delete GitHub App", then run `passlet github setup` again.
      """
    And the browser shows:
      """
      GitHub authorization was denied.
      Return to your terminal for next steps.
      """
    And Keychain contains no Passlet credentials

  Scenario: Wrong GitHub account is authorized
    Given Keychain contains no Passlet credentials
    And I have created `passlet-alice` during GitHub setup for `alice`
    When I authorize `passlet-alice` to act as `bob` on GitHub
    Then the terminal shows:
      """
      GitHub setup failed. Expected alice, but GitHub authorized bob. No credentials were saved.

      Your GitHub App still exists at https://github.com/settings/apps/passlet-alice/advanced.
      To start over, click "Delete GitHub App", then run `passlet github setup` again and authorize as alice.
      """
    And the browser shows:
      """
      GitHub setup failed. The authorized account does not match the requested GitHub handle.
      Return to your terminal for next steps.
      """
    And Keychain contains no Passlet credentials

  Scenario: Keychain fails to save credentials
    Given Keychain contains no Passlet credentials
    And I have created `passlet-alice` and authorized it to act as `alice` during GitHub setup
    And Keychain will fail to save the credentials
    When I install `passlet-alice` for `ACME` with access to all repositories
    And GitHub redirects my browser to "http://127.0.0.1:45821/github/installed" with the installation details
    Then the terminal shows:
      """
      Could not save credentials in your Mac's Keychain. GitHub setup failed.

      Your GitHub App still exists at https://github.com/settings/apps/passlet-alice/advanced.
      To start over, click "Delete GitHub App", then run `passlet github setup` again.
      """
    And the browser shows:
      """
      GitHub setup failed.
      Credentials could not be saved in your Mac's Keychain. Return to your terminal for next steps.
      """
    And Keychain contains no Passlet credentials

  Scenario: Installation gives no accessible repositories
    Given Keychain contains no Passlet credentials
    And I have created `passlet-alice` and authorized it to act as `alice` during GitHub setup
    And GitHub will report no accessible repositories for the installation
    When I install `passlet-alice` for `ACME` with access to all repositories
    And GitHub redirects my browser to "http://127.0.0.1:45821/github/installed" with the installation details
    Then the terminal shows:
      """
      GitHub setup failed. No accessible repositories were found for this installation. No credentials were saved.
      Check that your GitHub account has access to the repositories selected for the app installation.
      """
    And the browser shows:
      """
      GitHub setup failed. No accessible repositories were found.
      Return to your terminal for next steps.
      """
    And setup stops
    And Keychain contains no Passlet credentials

  Scenario: Install an existing GitHub App for an organization
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    When I run `passlet github setup`
    Then Passlet asks me to unlock its Keychain item
    When I approve with Touch ID
    Then the terminal shows:
      """
      Using your GitHub App: passlet-alice
      Signed in as: alice

      Choose an organization in your browser, then select "All repositories".

      If your browser didn't open:
      https://github.com/apps/passlet-alice/installations/new

      Waiting for installation…
      """
    And my browser opens "https://github.com/apps/passlet-alice/installations/new"
    When I install `passlet-alice` for `Globex` with access to all repositories
    Then GitHub redirects my browser to "http://127.0.0.1:45821/github/installed" with the installation details
    And the browser shows:
      """
      GitHub setup complete.
      You can close this tab and return to your terminal.
      """
    And the terminal shows:
      """
      GitHub access enabled for Globex.

      Run `passlet github setup` again to add another organization.
      """
    And the existing Keychain credentials remain unchanged

  # Confirm the request callback in a live GitHub walkthrough before implementation.
  # Integration reference: https://sparkwing.dev/docs/github-app/
  @planned
  Scenario: Organization installation needs owner approval
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    And `ACME` requires an organization owner to approve app installations
    When I run `passlet github setup`
    Then Passlet asks me to unlock its Keychain item
    When I approve with Touch ID
    Then my browser opens "https://github.com/apps/passlet-alice/installations/new"
    When I request installation of `passlet-alice` for `ACME` on GitHub
    And GitHub redirects my browser to "http://127.0.0.1:45821/github/installed?setup_action=request" without an installation ID
    Then the terminal shows:
      """
      Organization owner approval is required. This request has not enabled repository access.
      You can close this terminal. Check the installation request on GitHub for approval.
      """
    And the browser shows:
      """
      Organization owner approval is required.
      You can close this tab. Check the installation request on GitHub for approval.
      """
    And setup stops
    And the existing Keychain credentials remain unchanged

  Scenario: Localhost port is already occupied
    Given another process is listening on `127.0.0.1:45821`
    When I run `passlet github setup`
    Then the terminal shows:
      """
      Cannot start GitHub setup because localhost port 45821 is already in use.
      Close the application using that port, then run `passlet github setup` again.
      """
    And no browser opens

  Scenario: Cancel Keychain approval
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    When I run `passlet github setup`
    Then Passlet asks me to unlock its Keychain item
    When I cancel the Keychain prompt
    Then the terminal shows:
      """
      GitHub setup canceled.
      """
    And no browser opens
    And the existing Keychain credentials remain unchanged

  Scenario: GitHub App name is already taken
    Given Keychain contains no Passlet credentials
    And a GitHub App named `passlet-alice` already exists
    When I run `passlet github setup`
    Then the terminal prompts "Please enter your GitHub handle:"
    When I enter `alice` and press Enter
    Then the terminal shows:
      """
      A GitHub App named passlet-alice already exists, but its credentials aren't saved in this Mac's Keychain. Passlet cannot reconnect to an existing app.

      To create a replacement:
      1. Open https://github.com/settings/apps/passlet-alice/advanced
      2. Click "Delete GitHub App" and confirm. This deletes the app and its installations.
      3. Run `passlet github setup` again to create the app and grant it access to your repositories.
      """
    And setup stops
    And no browser opens
    And Keychain contains no Passlet credentials

  Scenario: Saved GitHub credentials are no longer valid
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    And GitHub rejects those credentials because the app was deleted or its authorization was revoked
    When I run `passlet github setup`
    Then Passlet asks me to unlock its Keychain item
    When I approve with Touch ID
    Then the terminal shows:
      """
      GitHub no longer accepts the saved credentials for passlet-alice. The app may have been deleted or its authorization revoked.
      Run `passlet github reset`, then `passlet github setup` to set up again.
      """
    And setup stops
    And no browser opens
    And the existing Keychain credentials remain unchanged

  Scenario: Reset with no saved GitHub credentials
    Given Keychain contains no Passlet credentials
    When I run `passlet github reset`
    Then the terminal shows:
      """
      No GitHub credentials are saved in Keychain. Nothing to do.
      """
    And no confirmation is requested
    And the command exits successfully

  Scenario: Reset GitHub credentials
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    When I run `passlet github reset`
    Then the terminal shows:
      """
      This removes Passlet's GitHub credentials from your Mac's Keychain.
      Your GitHub App and its installations will remain on GitHub.
      """
    And the terminal prompts "Remove GitHub credentials? [y/N]"
    When I enter `y` and press Enter
    Then Keychain contains no Passlet credentials
    And the terminal shows:
      """
      GitHub credentials removed. Run `passlet github setup` to set up again.

      Your GitHub App remains on GitHub: https://github.com/settings/apps/passlet-alice
      """
    And no browser opens

  Scenario: Cancel resetting GitHub credentials
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    When I run `passlet github reset`
    Then the terminal prompts "Remove GitHub credentials? [y/N]"
    When I press Enter
    Then the terminal shows:
      """
      GitHub reset canceled.
      """
    And the existing Keychain credentials remain unchanged

  Scenario: Refuse Keychain approval during reset
    Given Keychain contains credentials for `passlet-alice` authorized as `alice`
    And I will refuse the Keychain prompt
    When I run `passlet github reset`
    Then the terminal prompts "Remove GitHub credentials? [y/N]"
    When I enter `y` and press Enter
    Then the terminal shows:
      """
      GitHub reset canceled.
      """
    And the existing Keychain credentials remain unchanged
    And no browser opens

  Scenario: Override the suggested GitHub handle
    Given Keychain contains no Passlet credentials
    And Git's `github.user` setting is `alice`
    When I run `passlet github setup`
    Then the terminal prompts "Please enter your GitHub handle:"
    And the input is prefilled with `alice`
    When I enter `bob` and press Enter
    Then setup uses `bob` as my GitHub handle
