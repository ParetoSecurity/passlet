package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
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
var errSetupRequiresMac = errors.New("github_setup_requires_Mac")
var errGitHubAppNameTaken = errors.New("github_app_name_taken")
var errApproverRequiresMac = errors.New("approver_requires_the_macOS_build_of_Passlet")

func main() { // coverage-ignore: Process exit adapter; command behavior is tested through run and subprocess integration tests.
	if err := run(); err != nil {
		writeCommandError(os.Stderr, err)
		os.Exit(1)
	}
}
func writeCommandError(out io.Writer, err error) {
	if errors.Is(err, errSetupRequiresMac) {
		return
	}
	if errors.Is(err, errGitHubAppNameTaken) {
		_, _ = fmt.Fprintln(out)
		return
	}
	_, _ = fmt.Fprintln(out, "error:", err)
}
func run() error {
	return runWithOverview(overview)
}

func runWithOverview(showOverview func(context.Context) error) (result error) {
	defer func() {
		if errors.Is(result, flag.ErrHelp) {
			result = nil
		}
		if result != nil && !errors.Is(result, errApproverRequiresMac) && (len(os.Args) > 1 && os.Args[1] == "doctor" ||
			len(os.Args) > 2 && os.Args[1] == "approver" && os.Args[2] == "doctor") {
			result = fmt.Errorf("%w\n\nReport issues at https://github.com/ParetoSecurity/passlet/issues/", result)
		}
	}()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if len(os.Args) < 2 {
		fmt.Print(commandHelp)
		return nil
	}
	switch os.Args[1] {
	case "--help", "-h", "help":
		fmt.Print(commandHelp)
		return nil
	case "ssh-agent":
		return sshAgent(ctx, os.Args[2:])
	case "pair":
		return pair(ctx, os.Args[2:])
	case "doctor":
		return diagnostics(ctx, os.Args[2:])
	case "--version":
		fmt.Println("passlet", version)
		return nil
	case "approver":
		return approverCommand(ctx, os.Args[2:], showOverview)
	case "request", "exec":
		description := `Reuse scoped access from this Agent's private file cache, or request approval from your Approver. Print only a receipt by default. Use --stdout for grant JSON or --out for a private file for use with ` + "`passlet exec`" + `.

Request and use cached access:
  passlet request --repos alice/repo --permissions contents:read
  passlet exec --repos alice/repo --permissions contents:read -- gh repo view alice/repo

Fresh approval without cache lookup or storage (Bash/Zsh):
  passlet exec --grant <(passlet request --no-cache --stdout --repos alice/repo --permissions contents:read) -- gh repo view alice/repo`
		if os.Args[1] == "exec" {
			description = `Run a command with scoped GitHub access. Reuse a matching cached grant or request approval on a cache miss. Use --grant with a saved file or Bash/Zsh process substitution to run without requesting approval. Put the command after --.

Example:
  passlet exec --repos alice/repo --permissions contents:read -- gh repo view alice/repo`
		}
		fs := commandFlags(os.Args[1], description, "endpoint")
		var endpoint, repos, org, account, perms, out, grantFile string
		var noCache, stdout bool
		fs.StringVar(&endpoint, "endpoint", "", "override the environment, saved endpoint, or default local socket")
		fs.StringVar(&repos, "repos", "", "comma-separated owner/repository list")
		fs.StringVar(&org, "org", "", "organization-wide permission target (exclusive with --repos/--account)")
		fs.StringVar(&account, "account", "", "account-wide permission target (exclusive with --repos/--org)")
		fs.StringVar(&perms, "permissions", "", "comma-separated permission:read|write|admin list")
		if os.Args[1] == "request" {
			fs.BoolVar(&noCache, "no-cache", false, "bypass cache lookup and storage, requiring fresh approval on every call")
			fs.BoolVar(&stdout, "stdout", false, "write grant JSON to stdout")
			fs.StringVar(&out, "out", "", "write grant JSON to a new mode-0600 file")
		} else {
			fs.StringVar(&grantFile, "grant", "", "grant JSON file or process-substitution path (exclusive with scope options)")
		}
		if err := fs.Parse(os.Args[2:]); err != nil {
			return err
		}
		if stdout && out != "" {
			return errors.New("--stdout and --out are mutually exclusive")
		}
		if noCache && !stdout && out == "" {
			return errors.New("--no-cache requires --stdout or --out")
		}
		if grantFile != "" && (repos != "" || org != "" || account != "" || perms != "" || endpoint != "") {
			return errors.New("--grant and scope options are mutually exclusive")
		}
		if os.Args[1] == "exec" && grantFile == "" && repos == "" && org == "" && account == "" {
			return errors.New("exec requires --grant or scope options")
		}
		var grant passlet.Grant
		if grantFile != "" {
			f, err := os.Open(grantFile)
			if err != nil {
				return err
			}
			defer func() { _ = f.Close() }()
			if json.NewDecoder(io.LimitReader(f, 65536)).Decode(&grant) != nil || grant.Token == "" || !grant.ExpiresAt.After(time.Now()) {
				return errors.New("grant_expired_or_invalid")
			}
		} else {
			requested := passlet.Request{Agent: inferAgentName(), Organization: org, Account: account, Permissions: map[string]string{}}
			if repos != "" {
				requested.Repositories = strings.Split(repos, ",")
			}
			for _, p := range strings.Split(perms, ",") {
				kv := strings.Split(p, ":")
				if len(kv) != 2 {
					return errors.New("permissions must be comma-separated name:read|write|admin")
				}
				if _, ok := requested.Permissions[kv[0]]; ok {
					return errors.New("duplicate_permission")
				}
				requested.Permissions[kv[0]] = kv[1]
			}
			var err error
			endpoint, err = passlet.ResolveEndpoint(endpoint)
			if err != nil {
				return err
			}
			if os.Args[1] == "exec" && len(fs.Args()) == 0 {
				return errors.New("exec requires -- COMMAND [ARGS]")
			}
			if noCache {
				grant, err = passlet.RequestGrant(ctx, endpoint, requested)
			} else {
				grant, err = passlet.RequestCachedGrant(ctx, endpoint, requested)
			}
			if err != nil {
				return err
			}
		}
		fmt.Fprintf(os.Stderr, "Grant %s for %s expires %s\n", grant.ID, grant.User, grant.ExpiresAt.Format(time.RFC3339))
		if os.Args[1] == "request" {
			if out != "" {
				f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
				if err != nil {
					return err
				}
				defer func() { _ = f.Close() }()
				return json.NewEncoder(f).Encode(grant)
			}
			if stdout {
				return json.NewEncoder(os.Stdout).Encode(grant)
			}
			return nil
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
		defer func() { _ = os.RemoveAll(dir) }()
		self, err := os.Executable()
		if err != nil { // coverage-ignore: Requires the OS to lose the running executable identity.
			return err
		}
		wrapper := filepath.Join(dir, "credential")
		script := "#!/bin/sh\nexec '" + strings.ReplaceAll(self, "'", "'\\''") + "' git-credential \"$@\"\n"
		if err = os.WriteFile(wrapper, []byte(script), 0700); err != nil { // coverage-ignore: Requires disk failure or external interference inside a freshly created private directory.
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
		if os.Getenv("GIT_HTTP_PROXY_AUTHMETHOD") == "" {
			proxyMethod, err := exec.CommandContext(ctx, "git", "config", "--get-urlmatch", "http.proxyAuthMethod", "https://github.com").Output()
			if err == nil {
				method := strings.TrimSpace(string(proxyMethod))
				switch method {
				case "anyauth", "basic", "digest", "negotiate", "ntlm":
					env = append(env, "GIT_HTTP_PROXY_AUTHMETHOD="+method)
				}
			}
		}
		cmd.Env = env
		if err = cmd.Run(); err != nil {
			var exited *exec.ExitError
			if grantFile == "" && errors.As(err, &exited) && ctx.Err() == nil {
				if cacheErr := passlet.DiscardRejectedGrant(ctx, endpoint, grant); cacheErr != nil {
					return fmt.Errorf("command_failed: grant_cache_update_failed: %w", cacheErr)
				}
			}
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
