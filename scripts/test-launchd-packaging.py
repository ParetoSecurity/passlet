#!/usr/bin/env python3
"""Exercise the cask lifecycle against launchd with a disposable job, not Passlet."""
import os
from pathlib import Path
import plistlib
import re
import signal
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parent.parent
DOMAIN = f"gui/{os.getuid()}"


def main():
    subprocess.run(["/bin/launchctl", "print", DOMAIN], check=True, stdout=subprocess.DEVNULL)
    label = "dev.passlet.packaging-test." + uuid.uuid4().hex
    target = DOMAIN + "/" + label
    with tempfile.TemporaryDirectory(prefix="passlet-launchd-") as directory:
        root = Path(directory)
        home = root / "test-user"
        home.mkdir()
        app = root / "Fixture.app"
        binaries = app / "Contents/MacOS"
        binaries.mkdir(parents=True)
        for name in ("passlet", "PassletHelper"):
            path = binaries / name
            path.write_text("#!/bin/sh\nexec /bin/sleep 3600\n")
            path.chmod(0o755)
        script = root / "lifecycle.sh"
        script.write_text((ROOT / "packaging/homebrew/service.sh").read_text().replace("$HOME", "$FIXTURE_HOME").replace("dev.passlet.agent", label))
        env = dict(os.environ, FIXTURE_HOME=str(home))
        retained = home / "retained-fixture-data"
        retained.write_text("keep")

        def lifecycle(action):
            subprocess.run(["/bin/sh", str(script), action, str(app), "/opt/homebrew"], env=env, check=True)

        def pid(previous=None):
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                state = subprocess.run(["/bin/launchctl", "print", target], capture_output=True, text=True)
                match = re.search(r"\bpid = (\d+)", state.stdout)
                if match and int(match[1]) != previous:
                    return int(match[1])
                time.sleep(0.2)
            raise AssertionError("disposable launchd job did not start/restart")

        try:
            lifecycle("install")
            first = pid()
            file = home / "Library/LaunchAgents" / (label + ".plist")
            assert plistlib.loads(file.read_bytes())["Label"] == label
            # Kill only the PID obtained from this uniquely named test job.
            os.kill(first, signal.SIGKILL)
            restarted = pid(first)
            lifecycle("install")
            pid(restarted)
            lifecycle("uninstall")
            assert subprocess.run(["/bin/launchctl", "print", target], capture_output=True).returncode != 0
            assert not file.exists()
            assert retained.read_text() == "keep"
            print("PASS: launchd install, crash recovery, upgrade restart and uninstall")
        finally:
            subprocess.run(["/bin/launchctl", "bootout", target], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == "__main__":
    main()
