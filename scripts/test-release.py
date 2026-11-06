#!/usr/bin/env python3
import copy
import hashlib
import importlib.util
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
spec = importlib.util.spec_from_file_location("verify_release", ROOT / "scripts/verify-release.py")
release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(release)
spec = importlib.util.spec_from_file_location("verify_preparation", ROOT / "scripts/verify-preparation.py")
preparation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preparation)


class PreparationTests(unittest.TestCase):
    def setUp(self):
        self.sha = "a" * 40
        self.pr = {
            "state": "open",
            "base": {"ref": "main", "repo": {"full_name": preparation.REPOSITORY}},
            "head": {"sha": self.sha, "repo": {"full_name": preparation.REPOSITORY}},
        }
        self.run = {"head_sha": self.sha, "path": ".github/workflows/ci.yml", "event": "pull_request", "status": "completed", "conclusion": "success"}

    def test_accepts_exact_pr_head_with_passing_ci(self):
        preparation.verify(self.sha, [self.pr], {"workflow_runs": [self.run]})

    def test_rejects_forks_closed_prs_wrong_bases_and_stale_heads(self):
        for path, value in [
            (("state",), "closed"),
            (("head", "repo"), None),
            (("head", "repo", "full_name"), "someone/passlet"),
            (("head", "sha"), "b" * 40),
            (("base", "ref"), "another-branch"),
            (("base", "repo", "full_name"), "someone/passlet"),
        ]:
            with self.subTest(field=path):
                pr = copy.deepcopy(self.pr)
                target = pr
                for name in path[:-1]:
                    target = target[name]
                target[path[-1]] = value
                with self.assertRaisesRegex(ValueError, "open PR"):
                    preparation.verify(self.sha, [pr], {"workflow_runs": [self.run]})
        with self.assertRaisesRegex(ValueError, "open PR"):
            preparation.verify(self.sha, [], {"workflow_runs": [self.run]})

    def test_rejects_missing_failed_pending_or_unrelated_ci(self):
        for field, value in [("head_sha", "b" * 40), ("path", ".github/workflows/other.yml"), ("event", "push"), ("status", "in_progress"), ("conclusion", "failure")]:
            with self.subTest(field=field):
                run = dict(self.run, **{field: value})
                with self.assertRaisesRegex(ValueError, "latest PR CI"):
                    preparation.verify(self.sha, [self.pr], {"workflow_runs": [run, self.run]})
        with self.assertRaisesRegex(ValueError, "latest PR CI"):
            preparation.verify(self.sha, [self.pr], {"workflow_runs": []})


class PublicationTests(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name) / "repo"
        self.root.mkdir()
        self.artifacts = self.root.parent / "artifacts"
        self.artifacts.mkdir()
        for name in release.METADATA:
            target = self.root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(ROOT / name, target)
        shutil.copytree(ROOT / "packaging/homebrew", self.root / "packaging/homebrew")
        (self.root / "app.go").write_text("base source\n")
        self.git("init", "-q")
        self.git("config", "user.email", "fixture@example.com")
        self.git("config", "user.name", "Fixture")
        self.git("config", "core.hooksPath", "/dev/null")
        self.commit()
        self.base = self.git("rev-parse", "HEAD").strip()
        (self.root / "app.go").write_text("reviewed source\n")
        self.commit()
        source = self.git("rev-parse", "HEAD").strip()
        self.run = {
            "id": 123,
            "head_sha": source,
            "head_branch": "v1.2.3-prepare",
            "event": "push",
            "path": ".github/workflows/release.yml",
            "repository": {"full_name": "ParetoSecurity/passlet"},
            "status": "completed",
            "conclusion": "success",
        }
        sums = ""
        for platform, suffix in [("macos_universal", "zip"), ("linux_amd64", "tar.gz"), ("linux_arm64", "tar.gz")]:
            name = f"passlet_1.2.3_{platform}.{suffix}"
            data = platform.encode()
            (self.artifacts / name).write_bytes(data)
            sums += f"{hashlib.sha256(data).hexdigest()}  {name}\n"
        (self.artifacts / "passlet_1.2.3_SHA256SUMS").write_text(sums)
        release.package.generate("1.2.3", self.artifacts, self.root, 123, source, source=self.root)
        self.commit()

    def git(self, *args):
        return subprocess.check_output(["git", *args], cwd=self.root, text=True, stderr=subprocess.PIPE)

    def commit(self):
        self.git("add", ".")
        self.git("-c", "commit.gpgsign=false", "commit", "-qm", "fixture")

    def verify(self):
        release.verify("1.2.3", self.artifacts, self.run, self.root)

    def test_final_tag_matches_prepared_assets_and_both_plugins(self):
        self.verify()

    def test_accepts_squashed_source_and_metadata_without_ancestry(self):
        self.git("reset", "--soft", self.base)
        self.commit()
        result = subprocess.run(["git", "merge-base", "--is-ancestor", self.run["head_sha"], "HEAD"], cwd=self.root)
        self.assertEqual(result.returncode, 1)
        self.verify()

    def test_rejects_unprepared_changes_in_squash(self):
        self.git("reset", "--soft", self.base)
        (self.root / "app.go").write_text("unprepared squash resolution\n")
        self.commit()
        with self.assertRaisesRegex(ValueError, "Source changed"):
            self.verify()

    def test_rejects_wrong_or_unsuccessful_workflow(self):
        for key, value in {
            "id": 456,
            "head_sha": "0" * 40,
            "head_branch": "v9.9.9-prepare",
            "event": "pull_request",
            "path": ".github/workflows/ci.yml",
            "repository": {"full_name": "someone/else"},
            "status": "in_progress",
            "conclusion": "failure",
        }.items():
            with self.subTest(field=key):
                run = dict(self.run, **{key: value})
                with self.assertRaisesRegex(ValueError, "successful preparation"):
                    release.verify("1.2.3", self.artifacts, run, self.root)

    def test_rejects_source_changes_since_preparation(self):
        (self.root / "app.go").write_text("unprepared source\n")
        self.commit()
        with self.assertRaisesRegex(ValueError, "Source changed"):
            self.verify()

    def test_rejects_previous_release_metadata_or_plugin_versions(self):
        for name in sorted(release.METADATA):
            with self.subTest(file=name):
                target = self.root / name
                original = target.read_bytes()
                target.write_bytes(original.replace(b"1.2.3", b"1.2.2"))
                with self.assertRaisesRegex(ValueError, "Tagged metadata"):
                    self.verify()
                target.write_bytes(original)

    def test_rejects_missing_provenance(self):
        path = self.root / "packaging/release.json"
        manifest = json.loads(path.read_text())
        del manifest["preparation"]
        path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, "preparation run"):
            self.verify()

    def test_rejects_plugin_changes_beyond_version(self):
        for name in ["plugin.json", ".claude-plugin/plugin.json"]:
            with self.subTest(manifest=name):
                path = self.root / name
                original = path.read_bytes()
                manifest = json.loads(original)
                manifest["description"] = "unprepared plugin change"
                path.write_text(json.dumps(manifest, indent=2) + "\n")
                with self.assertRaisesRegex(ValueError, "beyond its version"):
                    self.verify()
                path.write_bytes(original)

    def test_rejects_corrupted_missing_and_extra_assets(self):
        archive = self.artifacts / "passlet_1.2.3_linux_amd64.tar.gz"
        original = archive.read_bytes()
        archive.write_bytes(b"corrupted")
        with self.assertRaisesRegex(ValueError, "checksums"):
            self.verify()
        archive.unlink()
        with self.assertRaisesRegex(ValueError, "exactly"):
            self.verify()
        archive.write_bytes(original)
        (self.artifacts / "unexpected").write_text("extra")
        with self.assertRaisesRegex(ValueError, "exactly"):
            self.verify()

    def test_rejects_consistently_replaced_archives_and_checksums(self):
        archive = self.artifacts / "passlet_1.2.3_linux_amd64.tar.gz"
        old = hashlib.sha256(archive.read_bytes()).hexdigest()
        archive.write_bytes(b"different signed build")
        new = hashlib.sha256(archive.read_bytes()).hexdigest()
        sums = self.artifacts / "passlet_1.2.3_SHA256SUMS"
        sums.write_text(sums.read_text().replace(old, new))
        with self.assertRaisesRegex(ValueError, "Tagged metadata"):
            self.verify()


if __name__ == "__main__":
    unittest.main()
