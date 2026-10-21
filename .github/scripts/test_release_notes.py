"""Exercise release-note generation against real, isolated Git histories."""

import subprocess
import tempfile
import unittest
from pathlib import Path

from release_notes import generate_notes


class ReleaseNotesTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.repo = Path(self.temp.name)
        self.git("init", "-b", "main")
        self.git("config", "user.name", "Release Test")
        self.git("config", "user.email", "test@example.invalid")

    def git(self, *args):
        return subprocess.check_output(
            ["git", *args], cwd=self.repo, encoding="utf-8", stderr=subprocess.DEVNULL
        ).strip()

    def commit(self, subject, body=None):
        args = ["commit", "--allow-empty", "-m", subject]
        if body:
            args.extend(["-m", body])
        self.git(*args)
        return self.git("rev-parse", "HEAD")

    def notes(self, tag):
        return generate_notes(tag, "example/musiccomments", cwd=self.repo)

    def test_range_categories_scopes_and_breaking_changes(self):
        self.commit("feat: old feature")
        self.git("tag", "-a", "v1.0.0", "-m", "First release")
        sha = self.commit("feat(music): 增加经典歌单")
        self.commit("fix(ui): 修复表情对齐")
        self.commit("docs: 更新截图")
        self.commit("refactor!: simplify client")
        self.commit("fix: handle shutdown", "BREAKING CHANGE: remove old configuration")
        self.git("tag", "v1.1.0")
        self.commit("feat: not released yet")
        notes = self.notes("v1.1.0")
        for text in ("## 新增功能", "music：增加经典歌单", "## Bug 修复", "ui：修复表情对齐",
                     "## 文档更新", "更新截图", "## 其他变更", "不兼容变更：simplify client",
                     "不兼容变更：handle shutdown", f"/commit/{sha}", "/compare/v1.0.0...v1.1.0"):
            self.assertIn(text, notes)
        self.assertNotIn("old feature", notes)
        self.assertNotIn("not released yet", notes)

    def test_first_release_at_root_and_plain_subject(self):
        self.commit("Initial implementation")
        self.git("tag", "v1.0.0")
        notes = self.notes("v1.0.0")
        self.assertIn("Initial implementation", notes)
        self.assertIn("首次发布", notes)
        self.assertNotIn("/compare/", notes)

    def test_prerelease_ignores_unrelated_and_nonversion_tags(self):
        self.commit("feat: first")
        self.git("tag", "v1.0.0")
        self.git("checkout", "-b", "unrelated")
        self.commit("feat: unrelated")
        self.git("tag", "v9.0.0")
        self.git("checkout", "main")
        self.commit("fix: included")
        self.git("tag", "backup")
        self.commit("feat: preview")
        self.git("tag", "v1.1.0-rc.1")
        notes = self.notes("v1.1.0-rc.1")
        self.assertIn("included", notes)
        self.assertNotIn("unrelated", notes)
        self.assertIn("/compare/v1.0.0...v1.1.0-rc.1", notes)

    def test_commit_text_is_escaped_and_never_executed(self):
        self.commit('fix: <script> *bold* [link](url) `$(echo injected)`')
        self.git("tag", "v1.0.0")
        notes = self.notes("v1.0.0")
        self.assertIn("&lt;script&gt;", notes)
        self.assertIn(r"\*bold\*", notes)
        self.assertIn(r"\[link\]", notes)
        self.assertIn(r"\`$(echo injected)\`", notes)


if __name__ == "__main__":
    unittest.main()
