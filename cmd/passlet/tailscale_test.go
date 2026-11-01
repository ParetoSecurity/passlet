package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func tailscaleConfigJSON(socket string) string {
	return fmt.Sprintf(`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Proxy":%q}}}}}`, "unix:"+socket)
}

func installTailscaleFixture(t *testing.T, socket string) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
case "$*" in
 'status --json') printf '%s' '{"Version":"1.104.4","BackendState":"Running","Self":{"DNSName":"mac.example.ts.net."}}' ;;
 'serve status --json') printf '%s' '` + strings.ReplaceAll(tailscaleConfigJSON(socket), "'", "'\\''") + `' ;;
 *) exit 99 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "tailscale"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestEnsureTailscale(t *testing.T) {
	const socket = "/tmp/passlet.sock.tailscale"
	const status = `{"Version":"1.104.4","BackendState":"Running","Self":{"DNSName":"mac.example.ts.net."}}`
	for _, tc := range []struct {
		name, initial        string
		wantWrite, wantError bool
	}{
		{"missing", `{}`, true, false},
		{"matching", tailscaleConfigJSON(socket), false, false},
		{"unrelated port", `{"TCP":{"443":{"HTTPS":true}},"Web":{"mac.example.ts.net:443":{"Handlers":{"/":{"Text":"keep"}}}},"AllowFunnel":{"mac.example.ts.net:443":true}}`, true, false},
		{"other backend", tailscaleConfigJSON("/tmp/other.sock"), false, true},
		{"raw TCP", `{"TCP":{"17400":{"TCPForward":"127.0.0.1:1234"}}}`, false, true},
		{"foreground", `{"Foreground":{"session":{"TCP":{"17400":{"HTTPS":true}}}}}`, false, true},
		{"Funnel", `{"AllowFunnel":{"mac.example.ts.net:17400":true}}`, false, true},
		{"web without TCP", `{"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Text":"keep"}}}}}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := tc.initial
			var writes [][]string
			err := ensureTailscale(context.Background(), socket, func(_ context.Context, name string, args ...string) ([]byte, error) {
				if name != "tailscale" {
					t.Fatalf("unexpected command %s", name)
				}
				switch strings.Join(args, " ") {
				case "status --json":
					return []byte(status), nil
				case "serve status --json":
					return []byte(config), nil
				default:
					writes = append(writes, args)
					config = tailscaleConfigJSON(socket)
					return nil, nil
				}
			})
			if (err != nil) != tc.wantError || (len(writes) == 1) != tc.wantWrite {
				t.Fatalf("writes %v error %v", writes, err)
			}
			if tc.wantWrite && !reflect.DeepEqual(writes[0], []string{"serve", "--bg", "--https=17400", "unix:" + socket}) {
				t.Fatalf("unexpected mutation %v", writes)
			}
		})
	}
}

func TestEnsureTailscaleFailures(t *testing.T) {
	for _, mode := range []string{"status failed", "status invalid", "stopped", "no hostname", "outdated", "config failed", "config invalid", "configure failed", "verification failed", "verification invalid", "verification read failed"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			err := ensureTailscale(context.Background(), "/tmp/ts.sock", func(_ context.Context, _ string, args ...string) ([]byte, error) {
				switch strings.Join(args, " ") {
				case "status --json":
					switch mode {
					case "status failed":
						return nil, errors.New("unavailable")
					case "status invalid":
						return []byte("invalid"), nil
					case "stopped":
						return []byte(`{"BackendState":"Stopped"}`), nil
					case "no hostname":
						return []byte(`{"BackendState":"Running"}`), nil
					case "outdated":
						return []byte(`{"Version":"1.104.1","BackendState":"Running","Self":{"DNSName":"mac.example.ts.net"}}`), nil
					}
					return []byte(`{"Version":"1.104.4","BackendState":"Running","Self":{"DNSName":"mac.example.ts.net"}}`), nil
				case "serve status --json":
					if mode == "config failed" || (mode == "verification read failed" && writes != 0) {
						return nil, errors.New("unavailable")
					}
					if mode == "config invalid" || (mode == "verification invalid" && writes != 0) {
						return []byte("invalid"), nil
					}
					return []byte(`{}`), nil
				default:
					writes++
					if mode == "configure failed" {
						return nil, errors.New("permission denied")
					}
					return nil, nil
				}
			})
			if err == nil {
				t.Fatal("failure reported success")
			}
		})
	}
}

func TestTailscaleSocketVersions(t *testing.T) {
	for _, tc := range []struct {
		version string
		want    bool
	}{
		{"1.104.4", true}, {"1.104.5", true}, {"1.106.0", true}, {"2.0.0", true}, {"1.104.4-build", true},
		{"1.104.1", false}, {"1.102.0", false}, {"0.104.4", false}, {"unknown", false}, {"1.x.4", false}, {"1.104", false},
	} {
		if got := supportsTailscaleSockets(tc.version); got != tc.want {
			t.Fatalf("%s: %v", tc.version, got)
		}
	}
}

func TestTailscaleRejectsAmbiguousForwarding(t *testing.T) {
	for _, data := range []string{
		`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Proxy":"unix:/tmp/socket"},"/other":{"Text":"other"}}}}}`,
		`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/other":{"Proxy":"unix:/tmp/socket"}}}}}`,
		`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Proxy":"unix:/tmp/socket","AcceptAppCaps":["cap"]}}}}}`,
		`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Proxy":"unix:/tmp/socket"}}},"other.example.ts.net:17400":{"Handlers":{"/":{"Text":"other"}}}}}`,
		`{"TCP":{"17400":{"HTTPS":true}},"Web":{"mac.example.ts.net:17400":{"Handlers":{"/":{"Proxy":"unix:/tmp/socket"}}}},"Foreground":{"s":{"TCP":{"17400":{"HTTPS":true}}}}}`,
	} {
		var config tailscaleServeConfig
		if err := json.Unmarshal([]byte(data), &config); err != nil {
			t.Fatal(err)
		}
		if config.forwardsTo("mac.example.ts.net", "/tmp/socket") {
			t.Fatal("ambiguous forwarding accepted")
		}
	}
}
