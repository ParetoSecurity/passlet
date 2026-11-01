#!/usr/bin/env python3
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import plistlib
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("package_release", ROOT / "scripts/package-release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)


class ReleaseTests(unittest.TestCase):
    def test_all_packages_pin_exact_artifacts(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            for platform, extension in [("macos_universal", "zip"), ("linux_amd64", "tar.gz"), ("linux_arm64", "tar.gz")]:
                (root / f"passlet_1.2.3_{platform}.{extension}").write_bytes(platform.encode())
            release.generate("1.2.3", root, root / "out")
            manifest = json.loads((root / "out/packaging/release.json").read_text())
            cask = (root / "out/Casks/passlet.rb").read_text()
            formula = (root / "out/Formula/passlet-agent.rb").read_text()
            for platform, artifact in manifest["artifacts"].items():
                digest = hashlib.sha256(platform.encode()).hexdigest()
                self.assertEqual(artifact["sha256"], digest)
                self.assertIn("/releases/download/v1.2.3/", artifact["url"])
                self.assertIn(digest, cask if platform == "macos_universal" else formula)
            self.assertNotIn("@VERSION@", cask + formula)
            self.assertNotIn(":no_check", cask + formula)

    def test_missing_artifacts_and_invalid_versions_write_nothing(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            with self.assertRaises(ValueError):
                release.generate("latest", root, root / "out")
            with self.assertRaises(FileNotFoundError):
                release.generate("1.2.3", root, root / "out")
            self.assertFalse((root / "out").exists())


@unittest.skipUnless(sys.platform == "darwin", "requires native macOS plutil")
class HomebrewLifecycleTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name)
        self.home = self.root / "Alice's & Mac"
        self.home.mkdir()
        self.app = self.root / "Passlet & Test.app"
        binaries = self.app / "Contents/MacOS"
        binaries.mkdir(parents=True)
        for name in ["passlet", "PassletHelper"]:
            p = binaries / name
            p.write_text("#!/bin/sh\nexit 0\n")
            p.chmod(0o755)
        self.state = self.root / "loaded"
        self.log = self.root / "calls"
        controller = self.root / "launchctl"
        controller.write_text('''#!/bin/sh
printf '%s\\n' "$*" >> "$FIXTURE_LOG"
case "$1" in
 print) case "$2" in */dev.passlet.agent) test -e "$FIXTURE_STATE";; *) exit 0;; esac ;;
 bootout) rm "$FIXTURE_STATE" ;;
 bootstrap) touch "$FIXTURE_STATE" ;;
 *) exit 1 ;;
esac
''')
        controller.chmod(0o755)
        # Rewrite only the fixture copy. Never invoke launchctl or change HOME.
        script = (ROOT / "packaging/homebrew/service.sh").read_text()
        script = script.replace('$HOME', '$FIXTURE_HOME').replace('/bin/launchctl', str(controller))
        self.script = self.root / "service.sh"
        self.script.write_text(script)
        self.file = self.home / "Library/LaunchAgents/dev.passlet.agent.plist"
        self.env = dict(os.environ, FIXTURE_HOME=str(self.home), FIXTURE_LOG=str(self.log), FIXTURE_STATE=str(self.state))

    def run_service(self, action):
        return subprocess.run(["/bin/sh", str(self.script), action, str(self.app), "/opt/homebrew"], env=self.env, capture_output=True, text=True)

    def test_install_upgrade_uninstall_preserves_credentials(self):
        secret = self.home / "retained-data"
        secret.write_text("fixture")
        result = self.run_service("install")
        self.assertEqual(result.returncode, 0, result.stderr)
        plist = plistlib.loads(self.file.read_bytes())
        self.assertEqual(plist["ProgramArguments"], [str(self.app / "Contents/MacOS/passlet"), "conductor", "--helper", str(self.app / "Contents/MacOS/PassletHelper")])
        self.assertNotIn("--tailscale", plist["ProgramArguments"])
        self.assertEqual(plist["LimitLoadToSessionType"], "Aqua")
        self.assertEqual(plist["AssociatedBundleIdentifiers"], ["dev.passlet.app"])
        self.assertEqual(plist["Umask"], 63)
        self.assertEqual(self.file.stat().st_mode & 0o777, 0o600)
        self.assertTrue(self.state.exists())
        result = self.run_service("install")
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("bootout", self.log.read_text())
        self.assertEqual(self.run_service("uninstall").returncode, 0)
        self.assertFalse(self.state.exists())
        self.assertFalse(self.file.exists())
        self.assertEqual(secret.read_text(), "fixture")
        self.assertEqual(self.run_service("uninstall").returncode, 0)

    def test_refuses_foreign_plist_and_symlink(self):
        self.file.parent.mkdir(parents=True)
        self.file.write_bytes(plistlib.dumps({"Label": "dev.passlet.agent"}))
        original = self.file.read_bytes()
        for action in ["install", "uninstall"]:
            self.assertNotEqual(self.run_service(action).returncode, 0)
            self.assertEqual(self.file.read_bytes(), original)
        self.file.unlink()
        self.file.symlink_to(self.root / "missing")
        self.assertNotEqual(self.run_service("install").returncode, 0)
        self.assertTrue(self.file.is_symlink())


if __name__ == "__main__":
    unittest.main()
