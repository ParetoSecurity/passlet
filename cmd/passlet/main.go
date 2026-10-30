package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"passlet.dev/passlet/internal/passlet"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "passlet:", err)
		os.Exit(1)
	}
}
func run() error {
	if len(os.Args) < 2 {
		return errors.New("usage: passlet serve | request | exec | import")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	switch os.Args[1] {
	case "version", "--version":
		fmt.Println("passlet", version)
		return nil
	case "serve":
		fs := flag.NewFlagSet("serve", flag.ContinueOnError)
		listen := fs.String("listen", "127.0.0.1:8484", "loopback PROXY-v2 listener")
		helper := fs.String("helper", "", "absolute path to signed Passlet helper executable")
		audit := fs.String("audit", "", "SQLite audit path")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if *helper == "" || !filepath.IsAbs(*helper) || *audit == "" {
			return errors.New("--helper (absolute) and --audit are required")
		}
		host, _, err := net.SplitHostPort(*listen)
		if err != nil || !net.ParseIP(host).IsLoopback() {
			return errors.New("listener_must_be_loopback")
		}
		a, err := passlet.OpenAudit(*audit)
		if err != nil {
			return errors.New("audit_unavailable")
		}
		defer a.Close()
		listener, err := net.Listen("tcp", *listen)
		if err != nil {
			return err
		}
		defer listener.Close()
		handler := passlet.NewServer(passlet.NativeHelper{Path: *helper}, passlet.NewGitHub(), a)
		server := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 125 * time.Second, IdleTimeout: 5 * time.Second, MaxHeaderBytes: 8192, BaseContext: func(net.Listener) context.Context { return ctx }}
		go func() { <-ctx.Done(); server.Close() }()
		fmt.Fprintln(os.Stderr, "Passlet listening on", *listen, "(trusted local PROXY v2 required)")
		err = server.Serve(&passlet.ProxyListener{Listener: listener})
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	case "import":
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		helper := fs.String("helper", "", "signed helper executable")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if !filepath.IsAbs(*helper) {
			return errors.New("absolute --helper required")
		}
		var c passlet.Credentials
		d := json.NewDecoder(io.LimitReader(os.Stdin, 65536))
		d.DisallowUnknownFields()
		if d.Decode(&c) != nil {
			return errors.New("invalid_credentials_json")
		}
		return passlet.ImportCredentials(ctx, *helper, c)
	case "request", "exec":
		fs := flag.NewFlagSet(os.Args[1], flag.ContinueOnError)
		endpoint := fs.String("endpoint", os.Getenv("PASSLET_ENDPOINT"), "Mac's Tailscale HTTPS origin")
		repos := fs.String("repos", "", "comma-separated owner/repository list")
		perms := fs.String("permissions", "", "comma-separated permission:read|write list")
		out := fs.String("out", "", "write grant JSON to a new mode-0600 file (request only)")
		grantFile := fs.String("grant", "", "existing grant JSON file (exec only)")
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		var grant passlet.Grant
		if *grantFile != "" {
			if os.Args[1] != "exec" || *repos != "" || *perms != "" {
				return errors.New("--grant is exclusive with request scope flags")
			}
			f, err := os.Open(*grantFile)
			if err != nil {
				return err
			}
			defer f.Close()
			if json.NewDecoder(io.LimitReader(f, 65536)).Decode(&grant) != nil || grant.Token == "" || !grant.ExpiresAt.After(time.Now()) {
				return errors.New("grant_expired_or_invalid")
			}
		} else {
			requested := passlet.Request{Repositories: strings.Split(*repos, ","), Permissions: map[string]string{}}
			for _, p := range strings.Split(*perms, ",") {
				kv := strings.Split(p, ":")
				if len(kv) != 2 {
					return errors.New("permissions must be comma-separated name:read|write")
				}
				if _, ok := requested.Permissions[kv[0]]; ok {
					return errors.New("duplicate_permission")
				}
				requested.Permissions[kv[0]] = kv[1]
			}
			var err error
			grant, err = passlet.RequestGrant(ctx, *endpoint, requested)
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "Grant %s for %s expires %s\n", grant.ID, grant.User, grant.ExpiresAt.Format(time.RFC3339))
		if os.Args[1] == "request" {
			if *out != "" {
				f, err := os.OpenFile(*out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return err
				}
				defer f.Close()
				return json.NewEncoder(f).Encode(grant)
			}
			return json.NewEncoder(os.Stdout).Encode(grant)
		}
		if *out != "" {
			return errors.New("--out is request-only")
		}
		args := fs.Args()
		if len(args) == 0 {
			return errors.New("exec requires -- COMMAND [ARGS]")
		}
		// Temporary credential helper serves the token only to github.com. Git receives
		// it via stdin/stdout, never via a URL, process argument, or persisted config.
		dir, err := os.MkdirTemp("", "passlet-git-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(dir)
		self, err := os.Executable()
		if err != nil {
			return err
		}
		wrapper := filepath.Join(dir, "credential")
		script := "#!/bin/sh\nexec '" + strings.ReplaceAll(self, "'", "'\\''") + "' git-credential \"$@\"\n"
		if err = os.WriteFile(wrapper, []byte(script), 0700); err != nil {
			return err
		}
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		env := []string{}
		for _, e := range os.Environ() {
			k := strings.SplitN(e, "=", 2)[0]
			if k == "GH_TOKEN" || k == "GITHUB_TOKEN" || k == "PASSLET_GIT_TOKEN" || strings.HasPrefix(k, "GIT_CONFIG_") || k == "GIT_ASKPASS" || k == "SSH_ASKPASS" || k == "GIT_TERMINAL_PROMPT" || strings.HasPrefix(k, "GH_HOST") {
				continue
			}
			env = append(env, e)
		}
		env = append(env, "GH_TOKEN="+grant.Token, "GITHUB_TOKEN="+grant.Token, "PASSLET_GIT_TOKEN="+grant.Token, "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_COUNT=2", "GIT_CONFIG_KEY_0=credential.helper", "GIT_CONFIG_VALUE_0=", "GIT_CONFIG_KEY_1=credential.https://github.com.helper", "GIT_CONFIG_VALUE_1="+wrapper)
		cmd.Env = env
		if err = cmd.Run(); err != nil {
			return errors.New("command_failed")
		}
		return nil
	case "git-credential":
		if len(os.Args) < 3 || os.Args[2] != "get" {
			return nil
		}
		data, err := io.ReadAll(io.LimitReader(os.Stdin, 8192))
		if err != nil {
			return err
		}
		fields := map[string]string{}
		for _, line := range strings.Split(string(data), "\n") {
			kv := strings.SplitN(line, "=", 2)
			if len(kv) == 2 {
				fields[kv[0]] = kv[1]
			}
		}
		if fields["protocol"] != "https" || fields["host"] != "github.com" {
			return nil
		}
		t := os.Getenv("PASSLET_GIT_TOKEN")
		if t != "" {
			fmt.Printf("username=x-access-token\npassword=%s\n\n", t)
		}
		return nil
	default:
		return errors.New("unknown command")
	}
}
