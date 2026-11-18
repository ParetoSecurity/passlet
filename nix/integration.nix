{ pkgs, source, agentArchive ? null }:
let
  passlet = import ./test-package.nix { inherit pkgs source; };
  # The Agent host runs the same build as the Linux release, or the
  # release archive itself when the Release workflow passes one in.
  agent = if agentArchive == null then passlet.overrideAttrs (_: { tags = [ "agentonly" ]; })
    else pkgs.runCommand "passlet-release-agent" { } ''
      mkdir -p $out/bin
      tar -xzf ${builtins.path { path = /. + agentArchive; name = "passlet-linux.tar.gz"; }} -C $out/bin passlet
    '';
  fixture = "${passlet}/libexec/passlet-vm-fixture -test.run=^TestVMFixture$ -test.timeout=30m";
  keys = pkgs.runCommand "passlet-test-only-keys" { nativeBuildInputs = [ pkgs.openssh pkgs.openssl ]; } ''
    mkdir -p $out
    for user in alice bob; do
      ssh-keygen -q -t ed25519 -N "" -f "$out/$user"
    done
    openssl req -x509 -newkey rsa:2048 -nodes -days 3650 \
      -keyout $out/server.key -out $out/server.crt -subj /CN=github.com \
      -addext 'subjectAltName=DNS:github.com,DNS:api.github.com'
  '';
  common = package: { ... }: {
    virtualisation.memorySize = 768;
    virtualisation.cores = 2;
    virtualisation.graphics = false;
    virtualisation.qemu.forceAccel = pkgs.lib.mkForce true;
    environment.systemPackages = [ package pkgs.git pkgs.gh pkgs.openssh pkgs.curl pkgs.python3 pkgs.sqlite ];
    networking.hosts."192.168.1.3" = [ "github.com" "api.github.com" ];
    security.pki.certificateFiles = [ "${keys}/server.crt" ];
  };
  approver = user: { ... }: {
    imports = [ (common passlet) ];
    networking.firewall.allowedTCPPorts = [ 17400 ];
    users.users.${user} = { isNormalUser = true; };
    systemd.services.passlet-fixture = {
      wantedBy = [ "multi-user.target" ];
      environment = { HOME = "/home/${user}"; PASSLET_VM_ROLE = user; SSL_CERT_FILE = "/etc/ssl/certs/ca-certificates.crt"; };
      serviceConfig = { User = user; ExecStart = fixture; Restart = "on-failure"; RestartSec = 1; };
    };
    systemd.services.ssh-key-agent = {
      serviceConfig = {
        User = user;
        ExecStart = "${pkgs.openssh}/bin/ssh-agent -D -a /home/${user}/.ssh/key-agent.sock";
      };
    };
    systemd.services.tunnel = {
      after = [ "network.target" "passlet-fixture.service" ];
      path = [ pkgs.openssh pkgs.coreutils ];
      preStart = ''
        mkdir -p /home/${user}/.ssh
        chmod 700 /home/${user}/.ssh
        cp ${keys}/${user} /home/${user}/.ssh/id_ed25519
        chmod 600 /home/${user}/.ssh/id_ed25519
      '';
      serviceConfig = {
        User = user;
        ExecStart = "${pkgs.openssh}/bin/ssh -NT -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ExitOnForwardFailure=yes -i /home/${user}/.ssh/id_ed25519 -R /home/${user}/.local/share/passlet/mac.sock:/home/${user}/.local/share/passlet/passlet.sock ${user}@hetzner";
      };
    };
  };
in pkgs.testers.runNixOSTest {
  name = "passlet-pairing-multi-user";
  globalTimeout = 900;
  nodes = {
    alice = approver "alice";
    bob = approver "bob";
    hetzner = { ... }: {
      imports = [ (common agent) ];
      # The fixture GitHub service lives here only for this isolated test.
      virtualisation.vlans = [ 1 ];
      networking.firewall.allowedTCPPorts = [ 443 ];
      services.openssh = {
        enable = true;
        settings = { PasswordAuthentication = false; AllowStreamLocalForwarding = "yes"; StreamLocalBindUnlink = "yes"; };
      };
      users.users.alice = { isNormalUser = true; extraGroups = [ "wheel" ]; openssh.authorizedKeys.keyFiles = [ "${keys}/alice.pub" ]; };
      users.users.bob = { isNormalUser = true; openssh.authorizedKeys.keyFiles = [ "${keys}/bob.pub" ]; };
      security.pam.sshAgentAuth.enable = true;
      security.sudo.extraConfig = ''
        Defaults env_keep += "SSH_AUTH_SOCK"
        Defaults timestamp_timeout=0
      '';
      systemd.services.fake-ssh-agent = {
        serviceConfig = {
          User = "alice";
          ExecStart = "${pkgs.openssh}/bin/ssh-agent -D -a /home/alice/.local/share/passlet/ssh-agent.sock";
        };
      };
      systemd.sockets.ssh-agent-proxy = {
        listenStreams = [ "/home/alice/.local/share/passlet/ssh-agent.sock" ];
        socketConfig = { Accept = true; MaxConnections = 32; RemoveOnStop = true; SocketMode = "0600"; SocketUser = "alice"; };
      };
      systemd.services."ssh-agent-proxy@" = {
        environment.HOME = "/home/alice";
        serviceConfig = { User = "alice"; ExecStart = "${agent}/bin/passlet ssh-agent --stdio"; StandardInput = "socket"; StandardOutput = "journal"; };
      };
      systemd.tmpfiles.rules = [
        "d /home/alice/.local 0700 alice users -" "d /home/alice/.local/share 0700 alice users -" "d /home/alice/.local/share/passlet 0700 alice users -"
        "d /home/bob/.local 0700 bob users -" "d /home/bob/.local/share 0700 bob users -" "d /home/bob/.local/share/passlet 0700 bob users -"
      ];
      systemd.services.github-fixture = {
        wantedBy = [ "multi-user.target" ];
        path = [ pkgs.git pkgs.coreutils ];
        preStart = ''
          mkdir -p /var/lib/test-github/acme
          for repo in test-a test-b; do
            git init --bare -b main /var/lib/test-github/acme/$repo.git
            git -C /var/lib/test-github/acme/$repo.git config http.receivepack true
          done
        '';
        environment = {
          PASSLET_VM_ROLE = "github";
          PASSLET_TEST_CERT = "${keys}/server.crt";
          PASSLET_TEST_KEY = "${keys}/server.key";
          PASSLET_TEST_GIT_BACKEND = "${pkgs.git}/libexec/git-core/git-http-backend";
        };
        serviceConfig.ExecStart = fixture;
      };
    };
  };
  testScript = ''
    import shlex

    start_all()
    hetzner.wait_for_unit("sshd.service")
    hetzner.wait_for_open_port(443)
    for machine in (alice, bob):
        machine.wait_for_unit("passlet-fixture.service")
        machine.succeed("systemctl start tunnel")
    for machine in (alice, bob):
        machine.succeed("systemctl is-active tunnel")
    for user in ("alice", "bob"):
        hetzner.wait_until_succeeds(f"test -S /home/{user}/.local/share/passlet/mac.sock")

    def as_user(user, command):
        return "su - " + user + " -c " + shlex.quote(command)

    def invite(machine, user):
        output = machine.succeed(as_user(user, "passlet approver pair"))
        return shlex.split(next(line for line in output.splitlines() if "passlet pair " in line))[-1]

    def pair(machine, user, endpoint, code, success=True):
        command = as_user(user, f"passlet pair --endpoint {shlex.quote(endpoint)} --code {shlex.quote(code)}")
        return machine.succeed(command) if success else machine.fail(command)

    def doctor(machine, user, success=True):
        command = as_user(user, "passlet doctor")
        if success:
            assert "GitHub authorization: valid" in machine.succeed(command)
        else:
            machine.fail(command)

    with subtest("Fresh local, SSH, and remote TLS Agents pair, revoke, and pair again"):
        for approver, user in ((alice, "alice"), (bob, "bob")):
            approver.wait_for_open_port(17400)
            assert "No paired Agents." in approver.succeed(as_user(user, "passlet approver agents list"))
            transports = (
                (approver, f"unix:///home/{user}/.local/share/passlet/passlet.sock", f"{user}@{user}"),
                (hetzner, f"unix:///home/{user}/.local/share/passlet/mac.sock", f"{user}@hetzner"),
                (hetzner, f"https://{user}:17400", f"{user}@hetzner"),
            )
            for agent, endpoint, label in transports:
                # Revoke the SSH pairing before exercising the same Agent over TCP.
                if endpoint.startswith("https:"):
                    approver.succeed(as_user(user, f"passlet approver agents revoke {label}"))
                agent.fail(as_user(user, f"passlet doctor --endpoint {endpoint}"))
                code = invite(approver, user)
                pair(agent, user, endpoint, code)
                assert label in approver.succeed(as_user(user, "passlet approver agents list")).splitlines()
                doctor(agent, user)
                saved = agent.succeed(f"sha256sum /home/{user}/.local/share/passlet/connection.json")
                prompts = approver.succeed(f"cat /home/{user}/.local/share/passlet/mode.prompts")
                pair(agent, user, endpoint, code, success=False)
                assert agent.succeed(f"sha256sum /home/{user}/.local/share/passlet/connection.json") == saved
                assert approver.succeed(f"cat /home/{user}/.local/share/passlet/mode.prompts") == prompts
                approver.succeed(as_user(user, f"passlet approver agents revoke {label}"))
                doctor(agent, user, success=False)
                pair(agent, user, endpoint, invite(approver, user))
                doctor(agent, user)
                for filename in ("agent.pem", "connection.json"):
                    agent.succeed(f"test $(stat -c %a /home/{user}/.local/share/passlet/{filename}) = 600")
            approver.succeed("systemctl restart passlet-fixture")
            approver.wait_for_open_port(17400)
            doctor(approver, user)
            doctor(hetzner, user)

    with subtest("Sudo uses the on-demand SSH-agent proxy without an SSH session"):
        alice.succeed("systemctl start ssh-key-agent")
        alice.wait_until_succeeds("test -S /home/alice/.ssh/key-agent.sock")
        alice.succeed(as_user("alice", "SSH_AUTH_SOCK=/home/alice/.ssh/key-agent.sock ssh-add /home/alice/.ssh/id_ed25519"))
        hetzner.succeed("systemctl start ssh-agent-proxy.socket")
        hetzner.wait_until_succeeds("test -S /home/alice/.local/share/passlet/ssh-agent.sock")
        no_proxy = "test -z \"$(systemctl list-units 'ssh-agent-proxy@*.service' --state=running --no-legend)\""
        hetzner.succeed(no_proxy)
        proxy_env = "SSH_AUTH_SOCK=/home/alice/.local/share/passlet/ssh-agent.sock "
        sudo = proxy_env + "sudo -k -S id -un </dev/null"
        alice.succeed("systemctl stop tunnel")
        assert hetzner.succeed(as_user("alice", sudo)).strip() == "root"
        hetzner.wait_until_succeeds(no_proxy)
        assert hetzner.succeed(as_user("alice", sudo)).strip() == "root"
        hetzner.wait_until_succeeds(no_proxy)
        hetzner.fail(as_user("bob", "SSH_AUTH_SOCK=/home/alice/.local/share/passlet/ssh-agent.sock ssh-add -L"))
        alice.succeed("systemctl restart passlet-fixture")
        alice.wait_for_open_port(17400)
        assert hetzner.succeed(as_user("alice", sudo)).strip() == "root"
        alice.succeed("systemctl start tunnel")
        hetzner.succeed("systemctl stop ssh-agent-proxy.socket")
        hetzner.succeed("systemctl start fake-ssh-agent")
        hetzner.wait_until_succeeds("test -S /home/alice/.local/share/passlet/ssh-agent.sock")
        hetzner.succeed(as_user("alice", "cp ${keys}/bob /home/alice/fake-key && chmod 600 /home/alice/fake-key"))
        hetzner.succeed(as_user("alice", proxy_env + "ssh-add /home/alice/fake-key"))
        hetzner.fail(as_user("alice", sudo))
        hetzner.succeed("systemctl stop fake-ssh-agent")
        hetzner.succeed("systemctl start ssh-agent-proxy.socket")

    with subtest("Replaced, expired, denied, and wrong-Approver invitations preserve the saved connection"):
        endpoint = "https://alice:17400"
        saved = hetzner.succeed("sha256sum /home/alice/.local/share/passlet/connection.json")
        old = invite(alice, "alice")
        code = invite(alice, "alice")
        prompts = alice.succeed("cat /home/alice/.local/share/passlet/mode.prompts")
        pair(hetzner, "alice", endpoint, old, success=False)
        pair(hetzner, "alice", "https://bob:17400", code, success=False)
        expire = "import json; p='/home/alice/.local/share/passlet/pairings.json'; d=json.load(open(p)); d['expires']='2000-01-01T00:00:00Z'; open(p,'w').write(json.dumps(d))"
        alice.succeed(as_user("alice", "python3 -c " + shlex.quote(expire)))
        pair(hetzner, "alice", endpoint, code, success=False)
        assert alice.succeed("cat /home/alice/.local/share/passlet/mode.prompts") == prompts
        code = invite(alice, "alice")
        alice.succeed("echo deny > /home/alice/.local/share/passlet/mode")
        pair(hetzner, "alice", endpoint, code, success=False)
        alice.succeed("rm /home/alice/.local/share/passlet/mode")
        pair(hetzner, "alice", endpoint, code, success=False)
        assert hetzner.succeed("sha256sum /home/alice/.local/share/passlet/connection.json") == saved
        doctor(hetzner, "alice")

    with subtest("A different key cannot replace an existing Agent label"):
        hetzner.succeed("cp -p /home/alice/.local/share/passlet/agent.pem /home/alice/.local/share/passlet/agent.pem.saved")
        try:
            hetzner.succeed("rm /home/alice/.local/share/passlet/agent.pem")
            pair(hetzner, "alice", "https://alice:17400", invite(alice, "alice"), success=False)
            doctor(hetzner, "alice", success=False)
        finally:
            hetzner.succeed("mv /home/alice/.local/share/passlet/agent.pem.saved /home/alice/.local/share/passlet/agent.pem")
        doctor(hetzner, "alice")

    def request(user, repo, name, permission="write"):
        return as_user(user, f"passlet request --no-cache --endpoint unix:///home/{user}/.local/share/passlet/mac.sock --repos acme/{repo} --permissions contents:{permission} --out /home/{user}/.local/share/passlet/{name}.json")

    def execute(user, grant, command):
        return as_user(user, f"passlet exec --grant /home/{user}/.local/share/passlet/{grant}.json -- {command}")

    with subtest("Independent Alice and Bob requests and private grant files"):
        hetzner.succeed(request("alice", "test-a", "write") + " > /tmp/alice-request.log 2>&1 & " + request("bob", "test-b", "write") + " > /tmp/bob-request.log 2>&1 & wait; cat /tmp/alice-request.log /tmp/bob-request.log")
        for user in ("alice", "bob"):
            hetzner.succeed(f"test $(stat -c %a /home/{user}/.local/share/passlet/write.json) = 600")
            # Metadata only: do not print test tokens, even in VM logs.
            hetzner.succeed(f"python3 -c \"import json; d=json.load(open('/home/{user}/.local/share/passlet/write.json')); assert d['user']=='{user}'; assert not any(k in d for k in ['access_token','refresh_token','client_secret'])\"")
        hetzner.fail(as_user("bob", "test -r /home/alice/.local/share/passlet/write.json"))
        hetzner.fail(as_user("bob", "passlet doctor --endpoint unix:///home/alice/.local/share/passlet/mac.sock") + " | grep 'GitHub authorization: valid'")

    with subtest("A different user's key cannot authenticate through Alice's tunnel"):
        count = "sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select count(*) from events'"
        before = alice.succeed(count)
        hetzner.succeed("cp -p /home/alice/.local/share/passlet/agent.pem /home/alice/.local/share/passlet/agent.pem.saved")
        try:
            hetzner.succeed("cat /home/bob/.local/share/passlet/agent.pem > /home/alice/.local/share/passlet/agent.pem")
            hetzner.fail(request("alice", "test-a", "wrong-key"))
            assert alice.succeed(count) == before, "untrusted key reached the approval handler"
        finally:
            hetzner.succeed("cat /home/alice/.local/share/passlet/agent.pem.saved > /home/alice/.local/share/passlet/agent.pem && rm /home/alice/.local/share/passlet/agent.pem.saved")

    with subtest("Real Git push and gh access are scoped"):
        hetzner.succeed(execute("alice", "write", "git clone https://github.com/acme/test-a.git /home/alice/checkout"))
        hetzner.succeed(as_user("alice", "cd /home/alice/checkout && git -c user.name=Alice -c user.email=alice@example.test commit --allow-empty -m fixture"))
        hetzner.succeed(execute("alice", "write", "git -C /home/alice/checkout push origin HEAD:main"))
        hetzner.succeed(execute("alice", "write", "gh api repos/acme/test-a --jq .actor") + " | grep alice")
        hetzner.succeed(execute("bob", "write", "gh api repos/acme/test-b --jq .actor") + " | grep bob")
        hetzner.fail(execute("alice", "write", "git ls-remote https://github.com/acme/test-b.git"))
        hetzner.fail(execute("alice", "write", "gh api repos/acme/test-b"))
        hetzner.succeed(request("alice", "test-a", "read", "read"))
        hetzner.fail(execute("alice", "read", "git -C /home/alice/checkout push origin HEAD:forbidden"))
        hetzner.fail(request("alice", "not-installed", "invalid"))

    with subtest("Denial, busy handling, and deadline cancellation"):
        alice.succeed("echo deny > /home/alice/.local/share/passlet/mode")
        hetzner.fail(request("alice", "test-a", "denied"))
        alice.succeed("echo wait > /home/alice/.local/share/passlet/mode")
        hetzner.succeed(request("alice", "test-a", "waiting") + " > /tmp/waiting.log 2>&1 &")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^requested$'")
        hetzner.fail(request("alice", "test-a", "busy") + " 2>/tmp/busy.log")
        hetzner.succeed("grep approver_busy /tmp/busy.log")
        hetzner.wait_until_succeeds("grep approval_timeout /tmp/waiting.log")

    with subtest("SSH disconnect cancels approval; existing grant survives"):
        hetzner.succeed(request("alice", "test-a", "disconnected") + " > /tmp/disconnected.log 2>&1 &")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^requested$'")
        alice.succeed("systemctl stop tunnel")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^request_cancelled$'")
        hetzner.succeed("test ! -e /home/alice/.local/share/passlet/disconnected.json")
        alice.succeed("systemctl stop passlet-fixture")
        hetzner.succeed(execute("alice", "write", "git ls-remote https://github.com/acme/test-a.git HEAD"))
        hetzner.succeed(execute("alice", "write", "gh api repos/acme/test-a"))

    with subtest("Crash recovery recreates the local socket"):
        bob.succeed("systemctl kill -s SIGKILL passlet-fixture")
        bob.wait_until_succeeds("su - bob -c 'passlet doctor' | grep 'GitHub authorization: valid'")

    with subtest("Audit never includes fixture credentials or tokens"):
        for machine, user in ((alice, "alice"), (bob, "bob")):
            machine.fail(f"sqlite3 /home/{user}/.local/share/passlet/audit.sqlite 'select data from events' | grep -qE 'parent-|fixture-client-secret|fixture-refresh|\"token\"'")

    with subtest("Provider expiry rejects a previously working token"):
        hetzner.succeed("curl --fail https://api.github.com/__expire")
        hetzner.fail(execute("alice", "write", "gh api repos/acme/test-a"))
  '';
}
