#!/usr/bin/env python3
"""Test Homebrew lifecycle in build/homebrew-validation with a disposable app."""
import hashlib
import os
from pathlib import Path
import plistlib
import re
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parent.parent
PREFIX = ROOT / "build/homebrew-validation"
BREW = PREFIX / "bin/brew"


def main():
    if not BREW.exists():
        raise SystemExit("First clone Homebrew/brew into build/homebrew-validation. Do not use your system Homebrew.")
    env = dict(os.environ, HOMEBREW_NO_ANALYTICS="1", HOMEBREW_NO_AUTO_UPDATE="1",
               HOMEBREW_CACHE=str(PREFIX / "test-cache"), HOMEBREW_LOGS=str(PREFIX / "test-logs"),
               HOMEBREW_USER_CONFIG_HOME=str(PREFIX / "test-config"))
    actual = subprocess.check_output([str(BREW), "--prefix"], env=env, text=True).strip()
    if Path(actual).resolve() != PREFIX.resolve():
        raise SystemExit("Refusing to use a non-test Homebrew prefix")
    tap = PREFIX / "Library/Taps/paretosecurity/homebrew-passlet"
    (tap / "Casks").mkdir(parents=True, exist_ok=True)
    subprocess.run(["git", "-C", str(tap), "init", "-q"], check=True)
    subprocess.run([str(BREW), "trust", "paretosecurity/passlet"], env=env, check=True)
    with tempfile.TemporaryDirectory(dir=ROOT / "build", prefix="brew-fixture-") as directory:
        root = Path(directory)
        user = root / "user"
        (user / "Library").mkdir(parents=True)
        app = root / "payload/Passlet.app"
        (app / "Contents/MacOS").mkdir(parents=True)
        (app / "Contents/Resources").mkdir()
        label = "dev.passlet.packaging-test." + uuid.uuid4().hex
        script = (ROOT / "packaging/homebrew/service.sh").read_text().replace("$HOME", str(user)).replace("dev.passlet.agent", label)
        # Run the generated fixture through the system shell. This test does not
        # exercise Gatekeeper or execute an unsigned downloaded app directly.
        script = script.replace('-insert ProgramArguments.0 -string "$app/Contents/MacOS/passlet"', '-insert ProgramArguments.0 -string /bin/sh')
        script = script.replace('-insert ProgramArguments.1 -string conductor', '-insert ProgramArguments.1 -string "$app/Contents/MacOS/passlet"')
        script = script.replace('-insert ProgramArguments.2 -string --helper', '-insert ProgramArguments.2 -string conductor')
        (app / "Contents/Resources/homebrew-service.sh").write_text(script)

        def publish_fixture(version):
            for name in ["passlet", "PassletHelper"]:
                path = app / "Contents/MacOS" / name
                path.write_text(f'#!/bin/sh\nif [ "$1" = --version ]; then echo "passlet {version}"; exit 0; fi\nexec /bin/sleep 3600\n')
                path.chmod(0o755)
            (app / "Contents/Info.plist").write_bytes(plistlib.dumps({
                "CFBundleIdentifier": label, "CFBundleExecutable": "passlet",
                "CFBundlePackageType": "APPL", "CFBundleName": "Passlet fixture", "CFBundleVersion": version,
            }))
            archive = root / f"fixture-{version}.zip"
            subprocess.run(["/usr/bin/ditto", "-c", "-k", "--keepParent", str(app), str(archive)], check=True)
            cask = (ROOT / "packaging/homebrew/passlet.rb.in").read_text()
            cask = cask.replace('cask "passlet"', 'cask "passlet-fixture"').replace("@VERSION@", version)
            cask = cask.replace("@MAC_SHA256@", hashlib.sha256(archive.read_bytes()).hexdigest())
            cask = cask.replace("https://github.com/ParetoSecurity/passlet/releases/download/v#{version}/passlet_#{version}_macos_universal.zip", archive.as_uri())
            (tap / "Casks/passlet-fixture.rb").write_text(cask)

        publish_fixture("0.0.0")
        target = f"gui/{os.getuid()}/{label}"
        token = "paretosecurity/passlet/passlet-fixture"

        def run(*args):
            subprocess.run([str(BREW), *args], env=env, check=True)

        def running_state():
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                state = subprocess.check_output(["/bin/launchctl", "print", target], text=True)
                if re.search(r"\bpid = \d+", state):
                    return state
                time.sleep(0.2)
            raise AssertionError("disposable package service is registered but not running")

        try:
            run("install", "--cask", token)
            running_state()
            assert (user / "Library/LaunchAgents" / f"{label}.plist").exists()
            run("reinstall", "--cask", token)
            running_state()
            publish_fixture("0.0.1")
            run("upgrade", "--cask", token)
            state = running_state()
            expected = str(PREFIX / "Caskroom/passlet-fixture/0.0.1/Passlet.app/Contents/MacOS/passlet")
            assert expected in state
            installed = subprocess.check_output(["/bin/sh", str(PREFIX / "bin/passlet"), "--version"], text=True)
            assert installed.strip() == "passlet 0.0.1"
            run("uninstall", "--cask", token)
            assert subprocess.run(["/bin/launchctl", "print", target], capture_output=True).returncode != 0
            assert not (user / "Library/LaunchAgents" / f"{label}.plist").exists()
            print("PASS: real Homebrew install/reinstall/upgrade/uninstall of disposable app")
        finally:
            subprocess.run(["/bin/launchctl", "bootout", target], capture_output=True)
            subprocess.run([str(BREW), "uninstall", "--cask", token], env=env, capture_output=True)


if __name__ == "__main__":
    main()
