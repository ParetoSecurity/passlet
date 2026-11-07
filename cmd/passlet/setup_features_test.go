package main

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/cucumber/godog"

	"passlet.dev/passlet/internal/passlet"
)

func TestGitHubSetupFeatures(t *testing.T) {
	suite := godog.TestSuite{
		Options: &godog.Options{Format: "pretty", Paths: []string{"features/github.feature"}, Strict: true, TestingT: t},
		ScenarioInitializer: func(s *godog.ScenarioContext) {
			type event struct{ action, output string }
			var f *setupFixture
			var started bool
			var occupied net.Listener
			var result setupResult
			var pending *event
			var actions chan event
			var proceed chan struct{}
			var finished chan setupResult
			s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
				f = newSetupFixture(t, "success")
				started = false
				occupied = nil
				pending = nil
				actions, proceed, finished = make(chan event), make(chan struct{}), nil
				result = setupResult{}
				return ctx, nil
			})
			await := func() error {
				if pending != nil {
					return nil
				}
				if finished == nil {
					return fmt.Errorf("setup is not running")
				}
				select {
				case e := <-actions:
					pending = &e
				case result = <-finished:
					finished = nil
					pending = &event{"done", result.output}
				case <-f.ctx.Done():
					return f.ctx.Err()
				}
				return nil
			}
			act := func(want string) error {
				if err := await(); err != nil {
					return err
				}
				if pending.action != want {
					return fmt.Errorf("waiting for %s, not %s", pending.action, want)
				}
				select {
				case proceed <- struct{}{}:
					pending = nil
					return nil
				case <-f.ctx.Done():
					return f.ctx.Err()
				}
			}
			contains := func(got, want string) error {
				if !strings.Contains(got, want) {
					return fmt.Errorf("expected text %q was not present", want)
				}
				return nil
			}
			s.Step(`^Keychain contains no Passlet credentials$`, func() error {
				if !started {
					if _, err := os.Stat(f.marker); !os.IsNotExist(err) {
						return fmt.Errorf("fixture Keychain is not empty")
					}
					return nil
				}
				if err := await(); err != nil {
					return err
				}
				if pending.action != "done" || result.keychain != "" {
					return fmt.Errorf("Keychain not empty")
				}
				return nil
			})
			s.Step(`^Passlet is installed on Linux$`, func() { f.hostOS = "linux" })

			s.Step(`^PASSLET_ENDPOINT is "([^"]*)"$`, func(endpoint string) { t.Setenv("PASSLET_ENDPOINT", endpoint) })
			s.Step(`^no GitHub handle is requested$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if f.prompts != 0 {
					return fmt.Errorf("unexpected terminal prompt")
				}
				return nil
			})
			s.Step(`^no GitHub callback listener starts$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if f.listenerStarts != 0 {
					return fmt.Errorf("Linux setup started a callback listener")
				}
				return nil
			})
			s.Step(`^no Keychain access or approval is requested$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if _, err := os.Stat(filepath.Join(f.dir, "helper-invoked")); !os.IsNotExist(err) {
					return fmt.Errorf("Linux setup invoked the Keychain helper: %v", err)
				}
				return nil
			})
			s.Step(`^no setup request is sent to the remote Approver$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if result.err != errSetupRequiresMac || f.github.requests.Load() != 0 {
					return fmt.Errorf("Linux setup did not stop before setup")
				}
				return nil
			})
			s.Step("^Git's `github.user` setting is `alice`$", func() { f.suggestedHandle = "alice" })
			start := func(command string) {
				started = true
				f.command = command
				finished = make(chan setupResult, 1)
				f.action = func(action string) error {
					select {
					case actions <- event{action, f.output.String()}:
					case <-f.ctx.Done():
						return f.ctx.Err()
					}
					select {
					case <-proceed:
						return nil
					case <-f.ctx.Done():
						return f.ctx.Err()
					}
				}
				go func() { finished <- f.run() }()
			}
			s.Step("^I run `passlet approver github (setup|reset)`$", start)
			s.Step(`^the terminal prompts "([^"]*)"$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				return contains(pending.output, want)
			})
			s.Step("^the input is prefilled with `([^`]+)`$", func(want string) error {
				if err := await(); err != nil {
					return err
				}
				if f.promptInitial != want {
					return fmt.Errorf("wrong editable suggestion")
				}
				return nil
			})
			s.Step(`^I press Enter$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if pending.action != "handle" && pending.action != "browser" {
					return fmt.Errorf("no terminal prompt awaiting Enter")
				}
				return act(pending.action)
			})
			s.Step("^setup uses `([^`]+)` as my GitHub handle$", func(handle string) error {
				if err := await(); err != nil {
					return err
				}
				if f.github.lookupName.Load() != "passlet-"+handle {
					return fmt.Errorf("wrong chosen handle")
				}
				return nil
			})
			s.Step(`^the terminal shows:$`, func(want *godog.DocString) error {
				if err := await(); err != nil {
					return err
				}
				text := want.Content
				if f.startURL != "" {
					u, _ := url.Parse(f.startURL)
					text = strings.ReplaceAll(text, "{session}", strings.Split(strings.TrimPrefix(u.Path, "/"), "/")[0])
				}
				if f.hostOS == "linux" && pending.output != text+"\n" {
					return fmt.Errorf("unexpected Linux setup output: %q; want %q", pending.output, text+"\n")
				}
				return contains(pending.output, text)
			})
			s.Step(`^my browser opens "([^"]*)"$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				parts := strings.Split(want, "{session}")
				for i := range parts {
					parts[i] = regexp.QuoteMeta(parts[i])
				}
				pattern := "^" + strings.Join(parts, `[A-Za-z0-9_-]+`) + "$"
				if !regexp.MustCompile(pattern).MatchString(f.lastURL) {
					return fmt.Errorf("unexpected browser destination")
				}
				return nil
			})
			s.Step(`^the same browser tab redirects to "([^"]*)"$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				if f.lastURL != want || f.opens != 1 {
					return fmt.Errorf("expected redirect in existing tab, without another browser launch")
				}
				return nil
			})
			s.Step(`^the page automatically submits the app configuration to "([^"]*)"$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				match := regexp.MustCompile(`action="([^"]+)"`).FindStringSubmatch(f.pageHTML)
				if len(match) != 2 {
					return fmt.Errorf("missing form")
				}
				u, _ := url.Parse(html.UnescapeString(match[1]))
				if u.Scheme+"://"+u.Host+u.Path != want {
					return fmt.Errorf("wrong manifest destination")
				}
				if err := contains(f.pageHTML, `method="post"`); err != nil {
					return err
				}
				return contains(f.pageHTML, `document.querySelector('form').submit();`)
			})
			s.Step(`^the app configuration sets the Setup URL to "([^"]*)"$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				match := regexp.MustCompile(`name="manifest" value="([^"]+)"`).FindStringSubmatch(f.pageHTML)
				if len(match) != 2 {
					return fmt.Errorf("missing manifest")
				}
				var manifest map[string]any
				if err := json.Unmarshal([]byte(html.UnescapeString(match[1])), &manifest); err != nil {
					return err
				}
				if manifest["setup_url"] != want || manifest["name"] != "passlet-alice" {
					return fmt.Errorf("wrong app manifest")
				}
				return nil
			})
			s.Step("^I confirm creating `passlet-alice` on GitHub$", func() error { return act("create") })
			s.Step("^I authorize `passlet-alice` to act as `(alice|bob)` on GitHub$", func(login string) error {
				if err := await(); err != nil {
					return err
				}
				if login == "bob" {
					f.mode = "wrong_account"
					f.github.mode = "wrong_account"
				}
				return act("authorize")
			})
			s.Step("^I install `passlet-alice` for `(ACME|Globex)` with access to all repositories$", func(owner string) error {
				if err := await(); err != nil {
					return err
				}
				f.github.owner = owner
				return act("install")
			})
			s.Step(`^GitHub redirects my browser to "([^"]*)" with the installation details$`, func(want string) error {
				if err := await(); err != nil {
					return err
				}
				if f.installCallback != want+"?installation_id=1" {
					return fmt.Errorf("wrong installation callback")
				}
				return nil
			})
			s.Step(`^Passlet verifies the installation with GitHub before saving credentials$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if f.github.verifications.Load() != 2 || !result.imported {
					return fmt.Errorf("verification/storage missing")
				}
				return nil
			})
			s.Step(`^credentials and verified identity are saved together in Keychain$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if result.err != nil || !result.imported || !result.accountSaved {
					return fmt.Errorf("protected import missing")
				}
				return nil
			})
			s.Step(`^the browser shows:$`, func(want *godog.DocString) error {
				if err := await(); err != nil {
					return err
				}
				for _, line := range strings.Split(want.Content, "\n") {
					if err := contains(html.UnescapeString(f.browserHTML), line); err != nil {
						return err
					}
				}
				return nil
			})
			configure := func(mode string) {
				f.mode = mode
				f.github.mode = mode
				f.helper = f.writeHelperLauncher()
			}
			s.Step("^Keychain contains credentials for `passlet-alice` authorized as `alice`$", func() error {
				configure("existing")
				return os.WriteFile(f.marker, []byte("existing"), 0600)
			})
			s.Step(`^Passlet asks me to unlock its Keychain item$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if pending.action != "keychain" {
					return fmt.Errorf("no Keychain approval requested")
				}
				return nil
			})
			s.Step(`^I approve with Touch ID$`, func() error { return act("keychain") })
			s.Step(`^I cancel the Keychain prompt$`, func() error {
				if err := await(); err != nil {
					return err
				}
				configure("protected")
				return act("keychain")
			})
			s.Step(`^the existing Keychain credentials remain unchanged$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if pending.action != "done" || result.keychain != "existing" || result.imported {
					return fmt.Errorf("existing credentials changed")
				}
				if result.exchanges != 0 {
					return fmt.Errorf("existing app went through OAuth again")
				}
				if f.command == "setup" && result.err == nil && result.browserOpens != 1 {
					return fmt.Errorf("expected only the installation picker to open")
				}
				return nil
			})
			s.Step(`^no browser opens$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if f.opens != 0 {
					return fmt.Errorf("unexpected browser launch")
				}
				return nil
			})
			s.Step(`^the local Agent is paired and its saved connection points to this Mac$`, func() error {
				if err := await(); err != nil {
					return err
				}
				agents, err := passlet.PairingAgents()
				if err != nil || len(agents) != 1 {
					return fmt.Errorf("expected one local Agent, got %v: %v", agents, err)
				}
				data, err := os.ReadFile(filepath.Join(passlet.StateDir(), "connection.json"))
				var connection struct{ Endpoint string }
				if err != nil {
					return err
				}
				if err := json.Unmarshal(data, &connection); err != nil {
					return err
				}
				if connection.Endpoint != "unix://"+passlet.DefaultSocket() {
					return fmt.Errorf("unexpected local connection: %s", connection.Endpoint)
				}
				return nil
			})
			s.Step(`^the command exits (successfully|unsuccessfully)$`, func(outcome string) error {
				if err := await(); err != nil {
					return err
				}
				if pending.action != "done" || (result.err == nil) != (outcome == "successfully") {
					return fmt.Errorf("expected command to exit %s, got action %q and error %v", outcome, pending.action, result.err)
				}
				return nil
			})
			s.Step(`^no confirmation is requested$`, func() error {
				if err := await(); err != nil {
					return err
				}
				if f.prompts != 0 {
					return fmt.Errorf("unexpected confirmation")
				}
				return nil
			})
			s.Step("^I enter `([^`]+)` and press Enter$", func(value string) error {
				if err := await(); err != nil {
					return err
				}
				switch pending.action {
				case "handle":
					f.handle = value
				default:
					return fmt.Errorf("not at an input prompt")
				}
				return act(pending.action)
			})
			s.Step("^a GitHub App named `passlet-alice` already exists$", func() { configure("name_conflict") })
			s.Step(`^GitHub rejects those credentials because the app was deleted or its authorization was revoked$`, func() { configure("revoked") })
			s.Step("^another process is listening on `127.0.0.1:17402`$", func() error {
				var err error
				occupied, err = net.Listen("tcp4", "127.0.0.1:17402")
				return err
			})
			created := func() error {
				start("setup")
				for _, step := range []string{"handle", "browser", "create"} {
					if err := act(step); err != nil {
						return err
					}
				}
				return await()
			}
			s.Step("^I have created `passlet-alice` during GitHub setup for `alice`$", created)
			s.Step("^I have created `passlet-alice` and authorized it to act as `alice` during GitHub setup$", func() error {
				if err := created(); err != nil {
					return err
				}
				if err := act("authorize"); err != nil {
					return err
				}
				return await()
			})
			s.Step(`^Keychain will fail to save the credentials$`, func() { configure("storage_failure") })
			s.Step(`^GitHub will report no accessible repositories for the installation$`, func() { configure("no_repositories") })
			s.Step(`^I will refuse the Keychain prompt$`, func() { configure("protected") })
			s.Step("^I deny authorization of `passlet-alice` on GitHub$", func() error {
				if err := await(); err != nil {
					return err
				}
				f.mode = "oauth_denied"
				return act("authorize")
			})
			s.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
				f.cancel()
				if occupied != nil {
					occupied.Close()
				}
				if finished != nil {
					result = <-finished
					finished = nil
				}
				if f.command == "reset" && (result.browserOpens != 0 || result.exchanges != 0) {
					t.Error("reset contacted setup flow")
				}
				assertSetupOutputContainsNoSecrets(t, result)
				return ctx, nil
			})
		},
	}
	if suite.Run() != 0 {
		t.Fatal("GitHub setup scenarios failed")
	}
}
