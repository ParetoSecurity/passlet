"""Pinned upstream advances replace links without copying or clobbering files."""
import importlib.util
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("conventions", Path(__file__).with_name("sync-agent-conventions.py"))
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)


class ConventionsTest(unittest.TestCase):
    def test_pin_updates_and_local_skill_wins(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            project = root / "project"
            (project / "skills" / "passlet").mkdir(parents=True)
            for version in ("old", "new"):
                upstream = root / version
                (upstream / "rules").mkdir(parents=True)
                (upstream / "skills" / "passlet").mkdir(parents=True)
                (upstream / "skills" / "review").mkdir(parents=True)
                with patch.object(module, "STORE_PREFIX", str(root) + "/"):
                    module.sync(upstream, project)
                self.assertEqual((project / ".claude/rules").resolve(), upstream / "rules")
                self.assertEqual((project / ".claude/skills/review").resolve(), upstream / "skills/review")
                self.assertEqual((project / ".claude/skills/passlet").resolve(), project / "skills/passlet")

    def test_preserve_project_owned_paths(self):
        with tempfile.TemporaryDirectory() as directory:
            target = Path(directory) / "rules"
            target.mkdir()
            with self.assertRaises(SystemExit):
                module.link(Path("/nix/store/example/rules"), target)
            self.assertTrue(target.is_dir())
            self.assertFalse(target.is_symlink())
            target.rmdir()
            target.symlink_to(Path(directory) / "my-custom-rules")
            with self.assertRaises(SystemExit):
                module.link(Path("/nix/store/example/rules"), target)
            self.assertEqual(target.readlink(), Path(directory) / "my-custom-rules")


if __name__ == "__main__":
    unittest.main()
