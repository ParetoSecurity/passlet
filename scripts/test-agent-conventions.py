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
    def test_pin_updates_and_local_skills_win(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            project = root / "project"
            (project / "skills" / "passlet").mkdir(parents=True)
            (project / ".agents" / "skills" / "release-passlet").mkdir(parents=True)
            (project / ".claude").symlink_to(".agents", target_is_directory=True)
            for version in ("old", "new"):
                upstream = root / version
                for name in ("pkgs", "rules", "commands", "skills/passlet", "skills/review"):
                    (upstream / name).mkdir(parents=True)
                if version == "old":
                    (upstream / "skills" / "retired").mkdir()
                with patch.object(module, "STORE_PREFIX", str(root) + "/"):
                    module.sync(upstream, project)
                self.assertEqual((project / ".agents/rules").resolve(), upstream / "rules")
                self.assertEqual((project / ".agents/commands").resolve(), upstream / "commands")
                self.assertEqual((project / ".claude/skills/review").resolve(), upstream / "skills/review")
                self.assertEqual((project / ".claude/skills/passlet").resolve(), project / "skills/passlet")
                self.assertTrue((project / ".agents/skills/release-passlet").is_dir())
                self.assertFalse((project / ".agents/skills/release-passlet").is_symlink())
                self.assertFalse((project / ".agents/pkgs").exists())
            self.assertFalse((project / ".agents/skills/retired").is_symlink())

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
