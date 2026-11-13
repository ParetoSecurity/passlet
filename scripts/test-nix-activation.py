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
            retained.parent.mkdir(parents=True)
            retained.write_text("keep")

            def activate():
                subprocess.run(["/bin/bash", "-c", script], env=env, check=True)

            activate()
            installed = home / "Library/LaunchAgents/dev.passlet.agent.plist"
            args = plistlib.loads(installed.read_bytes())["ProgramArguments"]
            self.assertEqual(args[0], str(app / "Contents/MacOS/passlet"))
            self.assertFalse(app.is_symlink())
            # macOS 15 cannot rename a directory with the Nix store's read-only mode.
            package.chmod(0o755)
            package.rename(root / "unmounted-store")
            subprocess.run([args[0], "--version"], check=True)
            subprocess.run([args[-1], "--version"], check=True)
            (root / "unmounted-store").rename(package)
            first = log.read_text()
            activate()
            self.assertEqual(log.read_text(), first)
            data = plistlib.loads(plist.read_bytes())
            data["PassletPackage"] += "-upgraded"
            plist.write_bytes(plistlib.dumps(data))
            activate()
            self.assertEqual(log.read_text().count("bootstrap"), 2)
            shutil.rmtree(app)
            activate()
            self.assertEqual(log.read_text().count("bootstrap"), 3)
            self.assertEqual(retained.read_text(), "keep")


if __name__ == "__main__":
    unittest.main(argv=[sys.argv[0]])
