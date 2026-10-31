import importlib.util
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location("quorum", Path(__file__).with_name("review-quorum.py"))
policy = importlib.util.module_from_spec(spec)
spec.loader.exec_module(policy)


def review(login, state="APPROVED", commit="head", id=1):
    return {"user": {"login": login}, "state": state, "commit_id": commit, "id": id}


class QuorumTests(unittest.TestCase):
    def test_author_counts_once(self):
        self.assertEqual(policy.quorum("zupo", "head", [review("zupo"), review("dz0ny")]), ["dz0ny", "zupo"])

    def test_external_author_needs_two(self):
        self.assertEqual(len(policy.quorum("outsider", "head", [review("dz0ny")])), 1)
        self.assertEqual(len(policy.quorum("outsider", "head", [review("dz0ny"), review("am-on")])), 2)

    def test_only_current_revision_and_maintainers(self):
        self.assertEqual(policy.quorum("outsider", "head", [review("dz0ny", commit="old"), review("other")]), [])

    def test_dismissal_and_change_requests_remove_approval(self):
        for state in ("DISMISSED", "CHANGES_REQUESTED"):
            self.assertEqual(policy.quorum("zupo", "head", [review("dz0ny"), review("dz0ny", state, id=2)]), ["zupo"])

    def test_comment_does_not_override_review(self):
        self.assertEqual(len(policy.quorum("zupo", "head", [review("dz0ny"), review("dz0ny", "COMMENTED", id=2)])), 2)


if __name__ == "__main__":
    unittest.main()
