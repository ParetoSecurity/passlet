package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cucumber/godog"
	"passlet.dev/passlet/internal/passlet"
)

func TestPairFeatures(t *testing.T) {
	suite := godog.TestSuite{Options: &godog.Options{Format: "pretty", Paths: []string{"features/pair.feature"}, Strict: true, TestingT: t}, ScenarioInitializer: func(s *godog.ScenarioContext) {
		var endpoint, code, output, prompt string
		var server *http.Server
		var denied bool
		s.Before(func(ctx context.Context, _ *godog.Scenario) (context.Context, error) {
			t.Setenv("HOME", t.TempDir())
			t.Setenv("PASSLET_ENDPOINT", "")
			denied = false
			server = nil
			return ctx, nil
		})
		s.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
			if server != nil {
				server.Close()
			}
			return ctx, err
		})
		s.Step(`^an Approver with a fresh pairing code$`, func() error {
			var err error
			code, err = passlet.CreateInvitation()
			if err != nil {
				return err
			}
			private, err := passlet.ReadPrivateKey(filepath.Join(passlet.StateDir(), "approver.pem"))
			if err != nil {
				return err
			}
			pairing, err := passlet.NewManagedPairing(private, func(_ context.Context, text string) error {
				prompt = text
				if denied {
					return errors.New("denied")
				}
				return nil
			})
			if err != nil {
				return err
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				return err
			}
			server = &http.Server{Handler: pairing.Handler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{"status":"ready"}`)) }))}
			go server.Serve(tls.NewListener(listener, pairing.ServerConfig()))
			endpoint = "https://" + listener.Addr().String()
			return nil
		})
		s.Step(`^pairing approval is denied$`, func() { denied = true })
		s.Step(`^the code has already been used$`, func() error { return passlet.PairAgent(context.Background(), endpoint, code, "alice@vm") })
		s.Step("^I run `passlet pair \\{endpoint\\} --code \\{code\\}`$", func() {
			var err error
			output, err = invokeCLI(t, "", "pair", endpoint, "--code", code)
			if err != nil {
				var message strings.Builder
				writeCommandError(&message, err)
				output += message.String()
			}
		})
		s.Step(`^the terminal shows:$`, func(want *godog.DocString) error {
			expected := strings.ReplaceAll(want.Content, "{endpoint}", endpoint) + "\n"
			if output != expected {
				return fmt.Errorf("got %q, want %q", output, expected)
			}
			return nil
		})
		s.Step(`^(?:the Agent can make authenticated requests|the existing connection still works)$`, func() error { return passlet.Health(context.Background(), endpoint) })
		s.Step(`^the Touch ID dialog shows:$`, func(want *godog.DocString) error {
			current, err := user.Current()
			if err != nil {
				return err
			}
			host, err := os.Hostname()
			if err != nil {
				return err
			}
			expected := strings.ReplaceAll(want.Content, "{agent}", current.Username+"@"+host)
			actual := "Passlet is trying to " + prompt + "."
			if actual != expected {
				return fmt.Errorf("got %q, want %q", actual, expected)
			}
			return nil
		})

		s.Step(`^no connection is saved$`, func() error {
			_, err := os.Stat(filepath.Join(passlet.StateDir(), "connection.json"))
			if !os.IsNotExist(err) {
				return errors.New("saved denied pairing")
			}
			return nil
		})
	}}
	if suite.Run() != 0 {
		t.Fatal("pairing scenarios failed")
	}
}
