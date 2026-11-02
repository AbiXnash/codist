#!/usr/bin/env python3
"""Measures Codist's reviewer against known bugs, so changes to models and prompts can be compared.

  python3 eval/run.py                                  the configured model
  python3 eval/run.py qwen2.5-coder:7b qwen2.5-coder:3b compare models
  python3 eval/run.py qwen2.5-coder:7b@8192             with a context size
  python3 eval/run.py --rules-only                     without any model

Needs server/bin/codist (make build) and, unless --rules-only, Ollama. It works on a temporary copy
and changes nothing in this repository. Results are also written to eval/results/<time>.json.
"""
import json, os, re, shutil, socket, subprocess, sys, tempfile, time, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
BIN = os.environ.get("CODIST_BIN") or os.path.join(HERE, "..", "server", "bin", "codist")
JAVA = "src/main/java/com/demo/library/"
RANK = ["info", "low", "medium", "high", "critical"]


def git(repo, *args):
    subprocess.run(["git", "-C", repo, "-c", "user.name=Eval", "-c", "user.email=eval@codist", *args], check=True, capture_output=True)


def get(port, path, data=None):
    req = urllib.request.Request(f"http://localhost:{port}/api{path}", data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def free_port():
    with socket.socket() as s:
        s.bind(("", 0))
        return s.getsockname()[1]


def score(findings, labels, repo):
    """Which labels were found, and which findings match nothing."""
    used = set()
    results = {}
    for bug in labels["bugs"]:
        path = JAVA + bug["file"]
        text = open(os.path.join(repo, path)).read().split("\n")
        at = next((i + 1 for i, l in enumerate(text) if bug["near"] in l), None)
        if at is None:
            raise SystemExit(f"label {bug['id']}: {bug['near']!r} is not in {path}")
        pattern = re.compile(bug["match"], re.I)
        hits = [i for i, f in enumerate(findings) if f["path"] == path and abs(f["line"] - at) <= 5 and pattern.search(f"{f['title']} {f.get('detail', '')}")]
        results[bug["id"]] = {"kind": bug["kind"], "found": bool(hits), "by": sorted({findings[i].get("source", "") for i in hits})}
        used.update(hits)
    also = re.compile(labels["alsoTrue"], re.I)
    noise = [f for i, f in enumerate(findings) if i not in used and not also.search(f["title"])]
    clean_paths = {JAVA + c for c in labels["clean"]}
    false_alarms = [f for f in findings if f["path"] in clean_paths and RANK.index(f["severity"]) >= RANK.index("low")]
    return results, noise, false_alarms


def run(config, labels):
    model, _, ctx = config.partition("@")
    rules_only = config == "--rules-only"
    work = tempfile.mkdtemp(prefix="codist-eval-")
    repo, data = os.path.join(work, "repo"), os.path.join(work, "data")
    shutil.copytree(os.path.join(HERE, "project"), repo)
    keep = os.path.join(repo, "src")
    held = os.path.join(work, "src")
    shutil.move(keep, held)
    git(repo, "init", "-q", "-b", "main")
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "base")
    shutil.move(held, keep)
    git(repo, "add", "-A")
    git(repo, "commit", "-qm", "add the library")
    sha = subprocess.check_output(["git", "-C", repo, "rev-parse", "HEAD"], text=True).strip()

    port = free_port()
    env = dict(os.environ, PORT=str(port), CODIST_DATA_DIR=data, CODIST_AUTO_REVIEW="false", CODIST_TOOLS="off", CODIST_MAX_FILES="100", CODIST_NO_BROWSER="1")
    if rules_only:
        env["OLLAMA_URL"] = "http://localhost:1"
    else:
        env["CODIST_MODEL"] = model
    if ctx:
        env["CODIST_NUM_CTX"] = ctx
    proc = subprocess.Popen([BIN, repo], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(50):
            try:
                get(port, "/status")
                break
            except OSError:
                time.sleep(0.2)
        start = time.time()
        get(port, f"/commits/{sha}/review", {})
        while True:
            time.sleep(2)
            review = get(port, f"/commits/{sha}").get("review") or {}
            if review.get("status") in ("done", "failed"):
                break
            if time.time() - start > 3600:
                raise SystemExit("gave up after an hour")
        took = time.time() - start
        findings = []
        for f in review.get("files") or []:
            for x in f.get("findings") or []:
                findings.append({**x, "path": f["path"]})
        calls = [a for a in get(port, "/activity") if a["kind"] not in ("step",)]
        results, noise, false_alarms = score(findings, labels, repo)
        return {"config": config, "seconds": round(took), "calls": len(calls), "tokensIn": sum(a.get("promptTokens") or 0 for a in calls),
                "tokensOut": sum(a.get("replyTokens") or 0 for a in calls), "findings": len(findings), "bugs": results,
                "all": [f"{f['severity']} [{f.get('source', '')}] {f['title']} ({os.path.basename(f['path'])}:{f['line']})" for f in findings],
                "noise": [f"{f['severity']} {f['title']} ({os.path.basename(f['path'])})" for f in noise],
                "falseAlarms": [f"{f['severity']} {f['title']} ({os.path.basename(f['path'])})" for f in false_alarms],
                "skipped": sum(1 for f in review.get("files") or [] if f.get("skipped"))}
    finally:
        proc.terminate()
        proc.wait()
        shutil.rmtree(work, ignore_errors=True)


def main():
    labels = json.load(open(os.path.join(HERE, "labels.json")))
    configs = sys.argv[1:] or [os.environ.get("CODIST_MODEL", "qwen2.5-coder:7b")]
    all_results = []
    for c in configs:
        print(f"running {c} …", flush=True)
        all_results.append(run(c, labels))
    kinds = sorted({b["kind"] for b in labels["bugs"]})
    print("\n%-28s %6s %6s %7s %5s %6s %6s  %s" % ("config", "found", "logic", "noise", "false", "calls", "secs", "missed"))
    for r in all_results:
        found = [k for k, v in r["bugs"].items() if v["found"]]
        logic = [v for v in r["bugs"].values() if v["kind"] == "logic"]
        missed = [k for k, v in r["bugs"].items() if not v["found"]]
        print("%-28s %3d/%-2d %3d/%-2d %7d %5d %6d %6d  %s" % (r["config"], len(found), len(r["bugs"]), sum(v["found"] for v in logic), len(logic), len(r["noise"]), len(r["falseAlarms"]), r["calls"], r["seconds"], ", ".join(missed)))
    out = os.path.join(HERE, "results")
    os.makedirs(out, exist_ok=True)
    path = os.path.join(out, time.strftime("%Y%m%d-%H%M%S") + ".json")
    json.dump(all_results, open(path, "w"), indent=2)
    print("\nDetails: " + os.path.relpath(path))


if __name__ == "__main__":
    main()
