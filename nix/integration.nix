{ pkgs, source }:
let
  passlet = import ./test-package.nix { inherit pkgs source; };
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
  common = { ... }: {
    virtualisation.memorySize = 768;
    virtualisation.cores = 2;
    virtualisation.graphics = false;
    virtualisation.qemu.forceAccel = pkgs.lib.mkForce true;
    environment.systemPackages = [ passlet pkgs.git pkgs.gh pkgs.openssh pkgs.curl pkgs.python3 pkgs.sqlite ];
    networking.hosts."192.168.1.3" = [ "github.com" "api.github.com" ];
    security.pki.certificateFiles = [ "${keys}/server.crt" ];
  };
  approver = user: { ... }: {
    imports = [ common ];
    users.users.${user} = { isNormalUser = true; };
    systemd.services.passlet-fixture = {
      wantedBy = [ "multi-user.target" ];
      environment = { HOME = "/home/${user}"; PASSLET_VM_ROLE = user; SSL_CERT_FILE = "/etc/ssl/certs/ca-certificates.crt"; };
      serviceConfig = { User = user; ExecStart = fixture; Restart = "on-failure"; RestartSec = 1; };
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
        ExecStart = "${pkgs.openssh}/bin/ssh -NT -o BatchMode=yes -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ExitOnForwardFailure=yes -i /home/${user}/.ssh/id_ed25519 -R /home/${user}/.local/share/passlet/mac.sock:/home/${user}/.local/share/passlet/passlet.sock ${user}@cruncher";
      };
    };
  };
in pkgs.testers.runNixOSTest {
  name = "passlet-ssh-multi-user";
  nodes = {
    alice = approver "alice";
    bob = approver "bob";
    cruncher = { ... }: {
      imports = [ common ];
      # The fixture GitHub service lives here only for this isolated test.
      virtualisation.vlans = [ 1 ];
      networking.firewall.allowedTCPPorts = [ 443 ];
      services.openssh = {
        enable = true;
        settings = { PasswordAuthentication = false; AllowStreamLocalForwarding = "yes"; StreamLocalBindUnlink = "yes"; };
      };
      users.users.alice = { isNormalUser = true; openssh.authorizedKeys.keyFiles = [ "${keys}/alice.pub" ]; };
      users.users.bob = { isNormalUser = true; openssh.authorizedKeys.keyFiles = [ "${keys}/bob.pub" ]; };
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
    # Nested KVM on Apple Silicon can boot more slowly than the driver's
    # fixed five-minute connect limit. Wait for readiness, never a fixed sleep.
    for machine in (alice, bob, cruncher):
        machine.wait_for_console_text("connecting to host", timeout=900)
    cruncher.wait_for_unit("sshd.service")
    cruncher.wait_for_open_port(443)
    for machine in (alice, bob):
        machine.wait_for_unit("passlet-fixture.service")
        machine.succeed("systemctl start tunnel")
    for machine in (alice, bob):
        machine.succeed("systemctl is-active tunnel")
    for user in ("alice", "bob"):
        cruncher.wait_until_succeeds(f"test -S /home/{user}/.local/share/passlet/mac.sock")

    def as_user(user, command):
        return "su - " + user + " -c " + shlex.quote(command)

    def request(user, repo, name, permission="write"):
        return as_user(user, f"passlet request --endpoint unix:///home/{user}/.local/share/passlet/mac.sock --repos acme/{repo} --permissions contents:{permission} --out /home/{user}/.local/share/passlet/{name}.json")

    def execute(user, grant, command):
        return as_user(user, f"passlet exec --grant /home/{user}/.local/share/passlet/{grant}.json -- {command}")

    with subtest("Independent Alice and Bob requests and private grant files"):
        cruncher.succeed(request("alice", "test-a", "write") + " > /tmp/alice-request.log 2>&1 & " + request("bob", "test-b", "write") + " > /tmp/bob-request.log 2>&1 & wait; cat /tmp/alice-request.log /tmp/bob-request.log")
        for user in ("alice", "bob"):
            cruncher.succeed(f"test $(stat -c %a /home/{user}/.local/share/passlet/write.json) = 600")
            # Metadata only: do not print test tokens, even in VM logs.
            cruncher.succeed(f"python3 -c \"import json; d=json.load(open('/home/{user}/.local/share/passlet/write.json')); assert d['user']=='{user}'; assert not any(k in d for k in ['access_token','refresh_token','client_secret'])\"")
        cruncher.fail(as_user("bob", "test -r /home/alice/.local/share/passlet/write.json"))
        cruncher.fail(as_user("bob", "passlet status --endpoint unix:///home/alice/.local/share/passlet/mac.sock") + " | grep 'Service: ready'")

    with subtest("Real Git push and gh access are scoped"):
        cruncher.succeed(execute("alice", "write", "git clone https://github.com/acme/test-a.git /home/alice/checkout"))
        cruncher.succeed(as_user("alice", "cd /home/alice/checkout && git -c user.name=Alice -c user.email=alice@example.test commit --allow-empty -m fixture"))
        cruncher.succeed(execute("alice", "write", "git -C /home/alice/checkout push origin HEAD:main"))
        cruncher.succeed(execute("alice", "write", "gh api repos/acme/test-a --jq .actor") + " | grep alice")
        cruncher.succeed(execute("bob", "write", "gh api repos/acme/test-b --jq .actor") + " | grep bob")
        cruncher.fail(execute("alice", "write", "git ls-remote https://github.com/acme/test-b.git"))
        cruncher.fail(execute("alice", "write", "gh api repos/acme/test-b"))
        cruncher.succeed(request("alice", "test-a", "read", "read"))
        cruncher.fail(execute("alice", "read", "git -C /home/alice/checkout push origin HEAD:forbidden"))
        cruncher.fail(request("alice", "not-installed", "invalid"))

    with subtest("Denial, busy handling, and deadline cancellation"):
        alice.succeed("echo deny > /home/alice/.local/share/passlet/mode")
        cruncher.fail(request("alice", "test-a", "denied"))
        alice.succeed("echo wait > /home/alice/.local/share/passlet/mode")
        cruncher.succeed(request("alice", "test-a", "waiting") + " > /tmp/waiting.log 2>&1 &")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^requested$'")
        cruncher.fail(request("alice", "test-a", "busy") + " 2>/tmp/busy.log")
        cruncher.succeed("grep approver_busy /tmp/busy.log")
        cruncher.wait_until_succeeds("grep approval_timeout /tmp/waiting.log")

    with subtest("SSH disconnect cancels approval; existing grant survives"):
        cruncher.succeed(request("alice", "test-a", "disconnected") + " > /tmp/disconnected.log 2>&1 &")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^requested$'")
        alice.succeed("systemctl stop tunnel")
        alice.wait_until_succeeds("sqlite3 /home/alice/.local/share/passlet/audit.sqlite 'select outcome from events order by id desc limit 1' | grep '^request_cancelled$'")
        cruncher.succeed("test ! -e /home/alice/.local/share/passlet/disconnected.json")
        alice.succeed("systemctl stop passlet-fixture")
        cruncher.succeed(execute("alice", "write", "git ls-remote https://github.com/acme/test-a.git HEAD"))
        cruncher.succeed(execute("alice", "write", "gh api repos/acme/test-a"))

    with subtest("Crash recovery recreates the local socket"):
        bob.succeed("systemctl kill -s SIGKILL passlet-fixture")
        bob.wait_until_succeeds("su - bob -c 'passlet status' | grep 'Service: ready'")

    with subtest("Audit never includes fixture credentials or tokens"):
        for machine, user in ((alice, "alice"), (bob, "bob")):
            machine.fail(f"sqlite3 /home/{user}/.local/share/passlet/audit.sqlite 'select data from events' | grep -qE 'parent-|fixture-client-secret|fixture-refresh|\"token\"'")

    with subtest("Provider expiry rejects a previously working token"):
        cruncher.succeed("curl --fail https://api.github.com/__expire")
        cruncher.fail(execute("alice", "write", "gh api repos/acme/test-a"))
  '';
}
