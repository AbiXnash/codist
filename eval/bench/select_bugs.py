#!/usr/bin/env python3
"""Chooses the bugs for the larger test set and writes manifest.json, which is then frozen.

  python3 eval/bench/select_bugs.py           choose and write the manifest
  python3 eval/bench/select_bugs.py --stats   only report what qualifies

Bugs come from GitBug-Java and Defects4J (both MIT), pinned to one commit of each. The manifest holds
only ids and splits, so no third-party code is stored here. A whole project goes to one split, so the
holdout tests projects the tuning never saw, and each project is capped so none dominates. Don't
regenerate the manifest once results have been compared.
"""
import hashlib, json, os, re, sys, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
DATASET = "gitbugactions/gitbug-java"
D4J = "rjust/defects4j"
# Defects4J projects with their commits on a public mirror. Jsoup is left out: GitBug-Java has it.
D4J_PROJECTS = {"Lang": "apache/commons-lang", "Math": "apache/commons-math", "Time": "JodaOrg/joda-time", "Cli": "apache/commons-cli",
                "Codec": "apache/commons-codec", "Collections": "apache/commons-collections", "Compress": "apache/commons-compress",
                "Csv": "apache/commons-csv", "Gson": "google/gson", "JacksonCore": "FasterXML/jackson-core",
                "JacksonDatabind": "FasterXML/jackson-databind", "JacksonXml": "FasterXML/jackson-dataformat-xml",
                "Mockito": "mockito/mockito", "JxPath": "apache/commons-jxpath"}
PER_PROJECT = 10
SEED = "codist-holdout-1"
HOLDOUT_SHARE = 0.3
MAX_CHANGED = 30
MAX_FILES = 3
CACHE = os.path.join(os.path.expanduser("~"), ".cache", "codist-bench")


def fetch(url):
    req = urllib.request.Request(url, headers={"User-Agent": "codist-eval"})
    with urllib.request.urlopen(req, timeout=60) as r:
        return r.read()


def load_bugs(commit):
    """Every bug in the dataset at a commit, cached on disk."""
    os.makedirs(CACHE, exist_ok=True)
    path = os.path.join(CACHE, f"gitbug-{commit[:12]}.json")
    if os.path.exists(path):
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    tree = json.loads(fetch(f"https://api.github.com/repos/{DATASET}/git/trees/{commit}?recursive=1"))
    bugs = []
    for item in tree["tree"]:
        if item["path"].startswith("data/bugs/") and item["path"].endswith(".json"):
            text = fetch(f"https://raw.githubusercontent.com/{DATASET}/{commit}/{item['path']}").decode()
            bugs += [json.loads(line) for line in text.splitlines() if line.strip()]
    with open(path, "w", encoding="utf-8") as f:
        json.dump(bugs, f)
    return bugs


def files_in(patch):
    """Each file in a unified diff with the lines it removes and adds, as text."""
    files, cur = [], None
    for line in patch.split("\n"):
        if line.startswith("diff --git"):
            cur = {"path": line.split(" b/", 1)[-1], "removed": [], "added": []}
            files.append(cur)
        elif cur is None or line.startswith(("--- ", "+++ ")):
            continue
        elif line.startswith("-"):
            cur["removed"].append(line[1:])
        elif line.startswith("+"):
            cur["added"].append(line[1:])
    return files


COMMENT = re.compile(r"^\s*(//|/\*|\*|$)")


def why_not(patch, buggy_side):
    """The reason a bug doesn't qualify, or "" if it does. buggy_side names the patch lines that hold
    the buggy code: "removed" for a patch from buggy to fixed, "added" for one from fixed to buggy."""
    files = files_in(patch or "")
    if not files or len(files) > MAX_FILES:
        return "too many files"
    if any(not f["path"].endswith(".java") or "/test/" in f["path"] for f in files):
        return "changes more than Java source"
    if sum(len(f["removed"]) + len(f["added"]) for f in files) > MAX_CHANGED:
        return "patch too large"
    # Re-adding the bug must add code, because a review looks at added lines. A fix that only added
    # code gives a bug of missing code, which this test can't place on a line.
    if not any(not COMMENT.match(l) for f in files for l in f[buggy_side]):
        return "fix only adds code"
    return ""


def order(key):
    return hashlib.sha256((SEED + key).encode()).hexdigest()


def d4j_patch(commit, project, number):
    return fetch(f"https://raw.githubusercontent.com/{D4J}/{commit}/framework/projects/{project}/patches/{number}.src.patch").decode(errors="replace")


def gitbug_candidates(commit):
    for b in sorted(load_bugs(commit), key=lambda x: order(x["commit_hash"])):
        yield ({"id": b["repository"].replace("/", "-") + "-" + b["commit_hash"][:8], "source": "gitbug-java", "repo": b["repository"],
                "cloneUrl": b["clone_url"], "fixed": b["commit_hash"]}, b.get("bug_patch"), "removed")


def d4j_candidates(commit):
    for project, repo in D4J_PROJECTS.items():
        lines = fetch(f"https://raw.githubusercontent.com/{D4J}/{commit}/framework/projects/{project}/commit-db").decode().split()
        rows = [l.split(",") for l in lines if "," in l]
        for row in sorted(rows, key=lambda r: order(project + r[0])):
            yield ({"id": f"{project}-{row[0]}", "source": "defects4j", "repo": repo, "cloneUrl": f"https://github.com/{repo}.git", "fixed": row[2],
                    "project": project, "number": row[0]}, (project, row[0]), "added")


def split_of(repo):
    h = hashlib.sha256((SEED + repo).encode()).digest()
    return "holdout" if h[0] / 256 < HOLDOUT_SHARE else "dev"


def main():
    manifest = os.path.join(HERE, "manifest.json")
    if os.path.exists(manifest) and "--stats" not in sys.argv:
        raise SystemExit("manifest.json exists and is frozen. Delete it only before any results are compared.")
    gitbug = json.loads(fetch(f"https://api.github.com/repos/{DATASET}/commits/main"))["sha"]
    d4j = json.loads(fetch(f"https://api.github.com/repos/{D4J}/commits/master"))["sha"]
    reasons, per_repo, chosen = {}, {}, []
    for gen, commit in ((gitbug_candidates, gitbug), (d4j_candidates, d4j)):
        for bug, patch, side in gen(commit):
            if per_repo.get(bug["repo"], 0) >= PER_PROJECT:
                reasons["project cap reached"] = reasons.get("project cap reached", 0) + 1
                continue
            if isinstance(patch, tuple):
                try:
                    patch = d4j_patch(commit, *patch)
                except OSError:
                    reasons["patch unavailable"] = reasons.get("patch unavailable", 0) + 1
                    continue
            why = why_not(patch, side)
            reasons[why or "qualifies"] = reasons.get(why or "qualifies", 0) + 1
            if not why:
                per_repo[bug["repo"]] = per_repo.get(bug["repo"], 0) + 1
                chosen.append({**bug, "split": split_of(bug["repo"])})
    chosen.sort(key=lambda b: b["id"])
    print(f"GitBug-Java at {gitbug[:12]}, Defects4J at {d4j[:12]}")
    for k, v in sorted(reasons.items(), key=lambda x: -x[1]):
        print(f"  {v:4d}  {k}")
    for s in ("dev", "holdout"):
        part = [b for b in chosen if b["split"] == s]
        print(f"{s}: {len(part)} bugs in {len({b['repo'] for b in part})} projects, {sum(b['source'] == 'gitbug-java' for b in part)} from GitBug-Java")
    if "--stats" in sys.argv:
        return
    with open(manifest, "w", encoding="utf-8") as f:
        json.dump({"gitbugCommit": gitbug, "defects4jCommit": d4j, "seed": SEED, "maxChanged": MAX_CHANGED, "maxFiles": MAX_FILES, "perProject": PER_PROJECT, "bugs": chosen}, f, indent=1)
    print("Wrote " + os.path.relpath(manifest))


if __name__ == "__main__":
    main()
