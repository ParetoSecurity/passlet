package passlet

import (
	"errors"
	"fmt"
	"strings"
)

// SetupError carries only validated identity and fixed messages, never provider
// response bodies or credentials. The CLI and browser render their own views.
type SetupError struct {
	Cause         error
	Terminal      string
	Browser       string
	ExpectedLogin string
}

func (e *SetupError) Error() string { return e.Cause.Error() }
func (e *SetupError) Unwrap() error { return e.Cause }

func setupFailure(err error, appURL string) *SetupError {
	e := &SetupError{Cause: err, Terminal: "GitHub setup failed. No credentials were saved.", Browser: "GitHub setup failed.\nReturn to your terminal for next steps."}
	var known *SetupError
	if errors.As(err, &known) {
		e.Terminal, e.Browser = known.Terminal, known.Browser
	}
	if err.Error() == "github_authorization_denied" {
		e.Terminal = "GitHub authorization was denied. No credentials were saved."
		e.Browser = "GitHub authorization was denied.\nReturn to your terminal for next steps."
	}
	if err.Error() == "github_app_installation_has_no_accessible_repositories" {
		e.Terminal = "GitHub setup failed. No accessible repositories were found for this installation. No credentials were saved.\nCheck that your GitHub account has access to the repositories selected for the app installation."
		e.Browser = "GitHub setup failed. No accessible repositories were found.\nReturn to your terminal for next steps."
	}
	if strings.HasPrefix(err.Error(), "keychain_import_failed") {
		e.Terminal = "Could not save credentials in your Mac's Keychain. GitHub setup failed."
		e.Browser = "GitHub setup failed.\nCredentials could not be saved in your Mac's Keychain. Return to your terminal for next steps."
	}
	if appURL != "" {
		e.Terminal += "\n\nYour GitHub App still exists at " + GitHubAppSettingsURL(appURL) + "/advanced.\nTo start over, click \"Delete GitHub App\", then run `passlet approver github setup` again."
		if known != nil && known.ExpectedLogin != "" {
			e.Terminal = strings.TrimSuffix(e.Terminal, ".") + " and authorize as " + known.ExpectedLogin + "."
		}
	}
	return e
}
func identityMismatch(expected, actual string) error {
	return &SetupError{ExpectedLogin: expected, Cause: errors.New("authorize_with_the_GitHub_account_that_created_the_app"), Terminal: fmt.Sprintf("GitHub setup failed. Expected %s, but GitHub authorized %s. No credentials were saved.", expected, actual), Browser: "GitHub setup failed. The authorized account does not match the requested GitHub handle.\nReturn to your terminal for next steps."}
}
