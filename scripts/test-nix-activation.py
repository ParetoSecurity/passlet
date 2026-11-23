#!/usr/bin/env python3
"""Exercise the evaluated Home Manager activation without touching the user's service."""
import json
import os
from pathlib import Path
import plistlib
import re
import shlex
import shutil
import subprocess
import sys
import tempfile
import unittest


class ActivationTests(unittest.TestCase):
    def test_tailscale_daemon_precedence_and_app_cli_mode(self):
        fixture = json.loads(Path(sys.argv[1]).read_text())
        environment = plistlib.loads(fixture["tailscalePlist"].encode())["EnvironmentVariables"]
        wrapper = Path(environment["PATH"].split(":")[0]) / "tailscale"
        with tempfile.TemporaryDirectory(prefix="passlet-tailscale-") as directory:
            root = Path(directory)
            app = root / "Tailscale.app/Contents/MacOS/Tailscale"
            app.parent.mkdir(parents=True)
            daemon_plist = root / "tailscaled.plist"
            fallback = root / "tailscale"
            for binary in [app, fallback]:
                binary.write_text('#!/bin/sh\nprintf "%s\\n" "$0" "${TAILSCALE_BE_CLI:-}" "$@"\nexit 42\n')
                binary.chmod(0o755)
            script = wrapper.read_text().replace("/Applications/Tailscale.app/Contents/MacOS/Tailscale", str(app))
            script = script.replace("/Library/LaunchDaemons/com.tailscale.tailscaled.plist", str(daemon_plist))
            script = re.sub(r"/nix/store/[^/]+/bin/tailscale", str(fallback), script)
            for native, daemon in [(True, True), (True, False), (False, True), (False, False)]:
                if not native and app.exists():
                    app.unlink()
                if daemon:
                    daemon_plist.touch()
                else:
                    daemon_plist.unlink(missing_ok=True)
                use_app = native and not daemon
                with self.subTest(native=native, daemon=daemon):
                    result = subprocess.run(["/bin/sh", "-c", script, "tailscale", "serve", "status", "--json"], env={"PATH": "/usr/bin:/bin"}, capture_output=True, text=True)
                    self.assertEqual(result.returncode, 42, result.stderr)
                    self.assertEqual(result.stdout.splitlines(), [str(app if use_app else fallback), "1" if use_app else "", "serve", "status", "--json"])

    def test_local_app_survives_missing_store_and_upgrades(self):
        fixture = json.loads(Path(sys.argv[1]).read_text())
        with tempfile.TemporaryDirectory(prefix="passlet-activation-") as directory:
            root = Path(directory)
            home = root / "Alice's Home"
            home.mkdir()
            package = root / "package"
            shutil.copytree(fixture["package"], package)
            plist = root / "agent.plist"
            plist.write_text(fixture["plist"].replace("/Users/alice", str(home)))
            log = root / "launchctl.log"
            controller = root / "launchctl"
            controller.write_text('#!/bin/sh\nprintf "%s\\n" "$*" >> "$FIXTURE_LOG"\n')
            controller.chmod(0o755)
            script = fixture["activation"].replace(
                fixture["plistFile"], str(plist)
            ).replace(fixture["package"], str(package)).replace(
                "/bin/launchctl", str(controller)
            )
            for name, path in {
                "app": home / ".local/share/passlet/Passlet.app",
                "plist": home / "Library/LaunchAgents/dev.passlet.agent.plist",
            }.items():
                script = re.sub(rf"^{name}=.*$", lambda _: f"{name}={shlex.quote(str(path))}", script, flags=re.MULTILINE)
            script = 'set -eu\nrun() { "$@"; }\n' + script
            env = dict(os.environ, FIXTURE_LOG=str(log))
            app = home / ".local/share/passlet/Passlet.app"
            retained = home / ".local/share/passlet/retained-credentials"

            def activate():
                subprocess.run(["/bin/bash", "-c", script], env=env, check=True)

            activate()
            self.assertEqual(app.parent.stat().st_mode & 0o777, 0o700)
            retained.write_text("keep")
            installed = home / "Library/LaunchAgents/dev.passlet.agent.plist"
            args = plistlib.loads(installed.read_bytes())["ProgramArguments"]
            self.assertEqual(args[0], str(app / "Contents/MacOS/passlet"))
            self.assertFalse(app.is_symlink())
            # The copied Nix store directory is read-only.
            package.chmod(0o755)
            package.rename(root / "unmounted-store")
            subprocess.run([args[0], "--version"], check=True)
            subprocess.run([args[-1], "--version"], check=True)
            (root / "unmounted-store").rename(package)
            first = log.read_text()
            app.parent.chmod(0o755)
            activate()
            self.assertEqual(app.parent.stat().st_mode & 0o777, 0o700)
            self.assertEqual(log.read_text(), first)
            data = plistlib.loads(plist.read_bytes())
            data["PassletPackage"] += "-upgraded"
            plist.write_bytes(plistlib.dumps(data))
            activate()
            self.assertEqual(log.read_text().count("bootstrap"), 2)
            shutil.rmtree(app)
            activate()
            self.assertEqual(log.read_text().count("bootstrap"), 3)
            self.assertEqual([line for line in log.read_text().splitlines() if line.startswith("bootout")], [f"bootout --wait gui/{os.getuid()}/dev.passlet.agent"] * 3)
            self.assertEqual(retained.read_text(), "keep")


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]])
