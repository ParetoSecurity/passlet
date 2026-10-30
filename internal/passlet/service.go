package passlet

import (
	"bytes"
	"encoding/xml"
	"errors"
	"path/filepath"
	"strings"
)

const ServiceLabel = "dev.passlet.agent"

// LaunchAgent uses argument arrays, never shell interpolation. SuccessfulExit
// restarts crashes but allows a clean termination to stay stopped until login.
func LaunchAgent(binary, helper, socket, audit, log, tcp, path string) ([]byte, error) {
	for _, value := range []string{binary, helper, socket, audit, log} {
		if !filepath.IsAbs(value) || strings.ContainsAny(value, "\x00\r\n") {
			return nil, errors.New("service_paths_must_be_absolute")
		}
	}
	escape := func(s string) string { var b bytes.Buffer; xml.EscapeText(&b, []byte(s)); return b.String() }
	args := []string{binary, "serve", "--socket", socket, "--helper", helper, "--audit", audit}
	if tcp != "" {
		args = append(args, "--tailscale-listen", tcp)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict><key>Label</key><string>` + ServiceLabel + `</string>
<key>ProgramArguments</key><array>`)
	for _, arg := range args {
		b.WriteString("<string>" + escape(arg) + "</string>")
	}
	b.WriteString(`</array><key>RunAtLoad</key><true/>
<key>KeepAlive</key><dict><key>SuccessfulExit</key><false/></dict>
<key>ThrottleInterval</key><integer>10</integer>
<key>LimitLoadToSessionType</key><string>Aqua</string>
<key>Umask</key><integer>63</integer>
<key>EnvironmentVariables</key><dict><key>PATH</key><string>` + escape(path) + `</string></dict>
<key>StandardOutPath</key><string>` + escape(log) + `</string>
<key>StandardErrorPath</key><string>` + escape(log) + `</string>
</dict></plist>`)
	return []byte(b.String()), nil
}
