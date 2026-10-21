"""Generate categorized release notes from commits between version tags."""

import argparse
import html
import re
import subprocess
from pathlib import Path
from urllib.parse import quote


VERSION_TAG = re.compile(r"v?\d+\.\d+\.\d+(?:-[0-9A-Za-z]+(?:\.[0-9A-Za-z]+)*)?\Z")
CONVENTIONAL = re.compile(
    r"(?P<type>[a-z]+)(?:\((?P<scope>[^)\r\n]+)\))?(?P<breaking>!)?:\s*(?P<title>.+)",
    re.IGNORECASE,
)
CATEGORIES = ("新增功能", "Bug 修复", "文档更新", "其他变更")


def git(*args, cwd=None):
    return subprocess.check_output(
        ["git", *args], cwd=cwd, encoding="utf-8", errors="strict"
    )


def escape_markdown(text):
    return re.sub(r"([\\`*_\[\]])", r"\\\1", html.escape(text, quote=False))


def generate_notes(tag, repository, cwd=None):
    if not VERSION_TAG.fullmatch(tag):
        raise ValueError(f"Unsupported version tag: {tag}")
    if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
        raise ValueError("Repository must be owner/name")
    commit = git("rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}", cwd=cwd).strip()
    # Search the parent history, excluding aliases on the current commit.
    previous = ""
    parent = subprocess.run(
        ["git", "rev-parse", "--verify", f"{commit}^"],
        cwd=cwd, capture_output=True, text=True, check=False,
    )
    if parent.returncode == 0:
        candidates = [
            name for name in git("tag", "--merged", parent.stdout.strip(), cwd=cwd).splitlines()
            if VERSION_TAG.fullmatch(name)
        ]
        if candidates:
            previous = git(
                "describe", "--tags", "--abbrev=0",
                *(f"--match={name}" for name in candidates), parent.stdout.strip(), cwd=cwd,
            ).strip()
    revision = f"refs/tags/{previous}..{commit}" if previous else commit
    raw = git("log", "--reverse", "--no-merges", "-z", "--format=%H%x00%s%x00%b", revision, cwd=cwd)
    fields = raw.split("\0")
    if fields and fields[-1] == "":
        fields.pop()
    if len(fields) % 3:
        raise ValueError("Unexpected git log record format")
    sections = {name: [] for name in CATEGORIES}
    base_url = f"https://github.com/{repository}"
    for index in range(0, len(fields), 3):
        sha, subject, body = fields[index:index + 3]
        category, title = "其他变更", subject
        match = CONVENTIONAL.fullmatch(subject)
        breaking = bool(re.search(r"(?m)^BREAKING[ -]CHANGE:", body))
        if match:
            category = {"feat": "新增功能", "fix": "Bug 修复", "docs": "文档更新"}.get(
                match["type"].lower(), "其他变更"
            )
            title = match["title"]
            if match["scope"]:
                title = f"{match['scope']}：{title}"
            breaking = breaking or bool(match["breaking"])
        if breaking:
            title = f"不兼容变更：{title}"
        sections[category].append(
            f"- {escape_markdown(title)} ([{sha[:7]}]({base_url}/commit/{sha}))"
        )
    lines = [f"# {tag}", ""]
    for category, entries in sections.items():
        if entries:
            lines.extend([f"## {category}", "", *entries, ""])
    if not any(sections.values()):
        lines.extend(["本版本没有新增提交。", ""])
    if previous:
        url = f"{base_url}/compare/{quote(previous, safe='')}...{quote(tag, safe='')}"
        label = f"{previous} → {tag}"
    else:
        url = f"{base_url}/commits/{quote(tag, safe='')}"
        label = "首次发布的提交记录"
    lines.extend([f"**完整变更**：[{label}]({url})", ""])
    return "\n".join(lines)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--tag", required=True)
    parser.add_argument("--repo", required=True)
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    args.output.write_text(generate_notes(args.tag, args.repo), encoding="utf-8", newline="\n")


if __name__ == "__main__":
    main()
