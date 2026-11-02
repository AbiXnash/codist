#!/usr/bin/env python3
"""Copies docs/wiki to the GitHub wiki, where page links have no .md ending.

  python3 scripts/publish_wiki.py             publish
  python3 scripts/publish_wiki.py --dry-run   convert the pages and report, without publishing

GitHub creates the wiki repository only when its first page is saved in the browser, so do that once.
"""
import os, re, shutil, subprocess, sys, tempfile

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
WIKI = os.path.join(ROOT, "docs", "wiki")
REMOTE = "https://github.com/AbiXnash/codist.wiki.git"
LINK = re.compile(r"\]\(([A-Za-z0-9_-]+)\.md(#[^)]*)?\)")


def convert(text):
    """Turns [Setup](Setup.md#x) into [Setup](Setup#x)."""
    text = text.replace("](../research/", "](https://github.com/AbiXnash/codist/blob/rebuild/docs/research/")
    return LINK.sub(lambda m: f"]({m.group(1)}{m.group(2) or ''})", text)


def copy_pages(dest):
    pages = sorted(n for n in os.listdir(WIKI) if n.endswith(".md"))
    for name in pages:
        with open(os.path.join(WIKI, name), encoding="utf-8") as f:
            text = convert(f.read())
        with open(os.path.join(dest, name), "w", encoding="utf-8", newline="\n") as f:
            f.write(text)
    return pages


def git(repo, *args):
    subprocess.run(["git", "-C", repo, *args], check=True)


def main():
    work = tempfile.mkdtemp(prefix="codist-wiki-")
    try:
        if "--dry-run" in sys.argv:
            pages = copy_pages(work)
            left = [p for p in pages if LINK.search(open(os.path.join(work, p), encoding="utf-8").read())]
            if left:
                raise SystemExit("links left unconverted in " + ", ".join(left))
            print(f"{len(pages)} pages would be published")
            return
        repo = os.path.join(work, "wiki")
        cloned = subprocess.run(["git", "clone", "-q", REMOTE, repo], capture_output=True, text=True)
        if cloned.returncode != 0:
            raise SystemExit("No wiki repository yet. Save a first page at https://github.com/AbiXnash/codist/wiki/_new, then run this again.")
        for name in os.listdir(repo):
            if name.endswith(".md"):
                os.remove(os.path.join(repo, name))
        pages = copy_pages(repo)
        git(repo, "add", "-A")
        if not subprocess.run(["git", "-C", repo, "status", "--porcelain"], capture_output=True, text=True).stdout.strip():
            print("The wiki is already up to date")
            return
        git(repo, "commit", "-q", "-m", "Update from docs/wiki")
        git(repo, "push", "-q")
        print(f"Published {len(pages)} pages")
    finally:
        shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    main()
