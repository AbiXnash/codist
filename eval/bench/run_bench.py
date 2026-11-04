#!/usr/bin/env python3
"""Runs Codist on real bugs from manifest.json, so results don't rest on the small seeded project.

  python3 eval/bench/run_bench.py --rules-only --limit 5    a quick check with no model
  python3 eval/bench/run_bench.py qwen2.5-coder:7b          all development bugs with one model
  python3 eval/bench/run_bench.py MODEL --resume FILE       continue a stopped run
  python3 eval/bench/run_bench.py MODEL --holdout           the frozen set: only to confirm a decision
  python3 eval/run.py --compare A.json B.json               paired test between two results

For each bug it takes the fixed commit and commits the bug back in, which gives a realistic change to
review. A finding within LOCATE lines of an added line in the same file counts as "located". That is an
upper bound, because a finding can sit near a bug without describing it, so sample results by hand before
quoting them. It then reviews the fix as well: findings there are candidate false alarms.

Nothing here changes this repository. Clones are kept in ~/.cache/codist-bench.
"""
import argparse, json, os, re, shutil, subprocess, sys, tempfile, time

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, os.path.dirname(HERE))
sys.path.insert(0, HERE)
import run as harness  # noqa: E402
import select_bugs as data  # noqa: E402

LOCATE = 3
REPOS = os.path.join(data.CACHE, "repos")
GIT = ["git", "-c", "core.autocrlf=false", "-c", "user.name=Eval", "-c", "user.email=eval@codist", "-c", "advice.detachedHead=false"]


def sh(*args, cwd=None):
    return subprocess.run(GIT + list(args), cwd=cwd, check=True, capture_output=True, text=True, errors="replace").stdout


def added_lines(diff):
    """The line numbers each file gains in a unified diff."""
    out, path, line = {}, None, 0
    for text in diff.split("\n"):
        if text.startswith("+++ "):
            name = text[4:].strip()
            path = None if name == "/dev/null" else name[2:] if name.startswith("b/") else name
            if path:
                out.setdefault(path, [])
        elif text.startswith("@@"):
            m = re.match(r"@@ -\d+(?:,\d+)? \+(\d+)", text)
            line = int(m.group(1)) if m else 0
        elif text.startswith("+") and path:
            if text[1:].strip():
                out[path].append(line)
            line += 1
        elif not text.startswith("-") and not text.startswith("\\"):
            line += 1
    return out


def located(findings, targets):
    """The findings within LOCATE lines of a target line in the same file."""
    return [f for f in findings if any(abs(f.get("line", 0) - t) <= LOCATE for t in targets.get(f["path"], []))]


def patch_of(bug, manifest):
    """The patch and whether it must be reversed to put the bug back into the fixed commit."""
    if bug["source"] == "defects4j":
        return data.d4j_patch(manifest["defects4jCommit"], bug["project"], bug["number"]), False
    for b in data.load_bugs(manifest["gitbugCommit"]):
        if b["commit_hash"] == bug["fixed"]:
            return b["bug_patch"], True
    raise KeyError(bug["id"])


def mirror(bug):
    path = os.path.join(REPOS, bug["repo"].replace("/", "__") + ".git")
    if not os.path.isdir(path):
        os.makedirs(REPOS, exist_ok=True)
        sh("clone", "-q", "--bare", "--filter=blob:none", bug["cloneUrl"], path)
    if subprocess.run(["git", "-C", path, "cat-file", "-e", bug["fixed"] + "^{commit}"], capture_output=True).returncode != 0:
        sh("fetch", "-q", "origin", bug["fixed"], cwd=path)
    return path


def review(repo, config, args, work):
    """Runs a server on the repo and reviews its HEAD. Returns (review, activity)."""
    rules_only = config == "--rules-only"
    sha = sh("rev-parse", "HEAD", cwd=repo).strip()
    port = harness.free_port()
    data_dir = tempfile.mkdtemp(prefix="data-", dir=work)
    model, _, ctx = config.partition("@")
    env = dict(os.environ, PORT=str(port), CODIST_DATA_DIR=data_dir, CODIST_AUTO_REVIEW="false", CODIST_NO_BROWSER="1", CODIST_TOOLS="off", CODIST_MAX_FILES="100")
    for name, var in harness.SWITCHES.items():
        if getattr(args, name):
            env[var] = "true"
    if rules_only:
        env["CODIST_MODEL_REVIEW"] = "false"
    else:
        env["CODIST_MODEL"] = model
    if ctx:
        env["CODIST_NUM_CTX"] = ctx
    proc = subprocess.Popen([harness.binary(), repo], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        up = time.monotonic()
        while True:
            try:
                harness.get(port, "/status")
                break
            except OSError:
                if time.monotonic() - up > 180:
                    raise RuntimeError("the server did not start")
                time.sleep(0.3)
        start = time.monotonic()
        harness.get(port, f"/commits/{sha}/review", {})
        while True:
            time.sleep(0.5 if rules_only else 2)
            rv = harness.get(port, f"/commits/{sha}").get("review") or {}
            if rv.get("status") in ("done", "failed"):
                break
            if time.monotonic() - start > args.timeout:
                rv = {**rv, "status": "timed out"}
                break
        rv["seconds"] = round(time.monotonic() - start)
        return rv, harness.get(port, "/activity")
    finally:
        proc.terminate()
        proc.wait()


def findings_of(rv):
    return [{**x, "path": f["path"]} for f in rv.get("files") or [] for x in f.get("findings") or []]


def describe(f):
    return f"{f.get('severity')} [{f.get('source', '')}] {f.get('title')} ({f['path']}:{f.get('line')})"


def run_bug(bug, config, args, manifest):
    patch, reverse = patch_of(bug, manifest)
    bare = mirror(bug)
    work = tempfile.mkdtemp(prefix="codist-bench-")
    tree = os.path.join(work, "repo")
    try:
        sh("worktree", "add", "-q", "--detach", tree, bug["fixed"], cwd=bare)
        sh("checkout", "-q", "-b", "eval-" + os.path.basename(work), cwd=tree)
        pfile = os.path.join(work, "bug.patch")
        with open(pfile, "w", encoding="utf-8", newline="\n", errors="surrogateescape") as f:
            f.write(patch if patch.endswith("\n") else patch + "\n")
        flags = ["--ignore-whitespace", "--whitespace=nowarn"] + (["-R"] if reverse else [])
        sh("apply", *flags, pfile, cwd=tree)
        sh("add", "-A", cwd=tree)
        sh("commit", "-q", "-m", "Change behaviour", cwd=tree)
        targets = added_lines(sh("diff", "HEAD~1", "HEAD", cwd=tree))
        rv, activity = review(tree, config, args, work)
        found = findings_of(rv)
        hits = located(found, targets)
        out = {"found": bool(hits), "kind": "real", "by": sorted({h.get("source", "") for h in hits}), "status": rv.get("status"), "seconds": rv.get("seconds"),
               "findings": len(found), "hits": [describe(h) for h in hits], "all": [describe(f) for f in found], "targets": targets,
               "details": [{k: f.get(k) for k in ("path", "line", "severity", "source", "title", "detail", "snippet", "ref", "doubt")} for f in found],
               "calls": harness.model_calls(activity), "drops": harness.drops(activity), "failedFiles": [f["path"] for f in rv.get("files") or [] if f.get("error")]}
        if not args.no_clean:
            sh("apply", *["--ignore-whitespace", "--whitespace=nowarn"] + ([] if reverse else ["-R"]), pfile, cwd=tree)
            sh("add", "-A", cwd=tree)
            sh("commit", "-q", "-m", "Fix behaviour", cwd=tree)
            fixed_targets = added_lines(sh("diff", "HEAD~1", "HEAD", cwd=tree))
            rv2, activity2 = review(tree, config, args, work)
            alarms = located(findings_of(rv2), fixed_targets)
            out.update(falseAlarms=[describe(f) for f in alarms], cleanStatus=rv2.get("status"), cleanSeconds=rv2.get("seconds"), cleanCalls=harness.model_calls(activity2))
        return out
    finally:
        subprocess.run(GIT + ["worktree", "remove", "--force", tree], cwd=bare, capture_output=True)
        subprocess.run(GIT + ["worktree", "prune"], cwd=bare, capture_output=True)
        shutil.rmtree(work, ignore_errors=True)


def summary(result, bugs):
    done = {k: v for k, v in result["bugs"].items() if "error" not in v}
    found = sum(v["found"] for v in done.values())
    by_rule = sum("rule" in v.get("by", []) for v in done.values())
    by_llm = sum("llm" in v.get("by", []) for v in done.values())
    only_llm = sum(v.get("by") == ["llm"] for v in done.values())
    secs = sum((v.get("seconds") or 0) + (v.get("cleanSeconds") or 0) for v in done.values())
    alarms = [v for v in done.values() if "falseAlarms" in v]
    print(f"\n{result['config']}: {len(done)} of {len(bugs)} bugs ran, {len(result['bugs']) - len(done)} could not")
    print(f"  Located (upper bound)  {harness.percent(found, len(done))}")
    print(f"  By a rule              {by_rule}")
    print(f"  By the model           {by_llm}, of which only the model: {only_llm}")
    if alarms:
        print(f"  On the fix: {sum(len(v['falseAlarms']) for v in alarms)} candidate false alarms in {len(alarms)} fixes, {sum(bool(v['falseAlarms']) for v in alarms)} fixes with any")
    print(f"  Findings per bug       {sum(v.get('findings', 0) for v in done.values()) / max(1, len(done)):.1f}")
    print(f"  Time                   {secs} s")
    per = {}
    for b in bugs:
        v = done.get(b["id"])
        if v:
            a = per.setdefault(b["repo"], [0, 0])
            a[0] += v["found"]
            a[1] += 1
    print("  By project: " + ", ".join(f"{k.split('/')[-1]} {a}/{n}" for k, (a, n) in sorted(per.items())))


def main():
    ap = argparse.ArgumentParser(description="Measure Codist on real bugs.")
    ap.add_argument("config", nargs="?", help="MODEL or MODEL@CONTEXT")
    ap.add_argument("--rules-only", action="store_true")
    for name in harness.SWITCHES:
        ap.add_argument("--" + name.replace("_", "-"), action="store_true")
    ap.add_argument("--holdout", action="store_true", help="use the frozen holdout, only to confirm a decision")
    ap.add_argument("--no-clean", action="store_true", help="skip reviewing the fixes, which halves the time")
    ap.add_argument("--limit", type=int, help="only the first N bugs")
    ap.add_argument("--only", help="only this bug id")
    ap.add_argument("--resume", help="a results file to continue")
    ap.add_argument("--timeout", type=int, default=1800, help="seconds one review may take while the machine is awake")
    args = ap.parse_args()
    args.default_settings = False
    config = "--rules-only" if args.rules_only else args.config
    if not config:
        ap.error("name a model or use --rules-only")
    if not os.path.exists(harness.binary()):
        raise SystemExit(f"{harness.binary()} is missing; run make build")
    with open(os.path.join(HERE, "manifest.json"), encoding="utf-8") as f:
        manifest = json.load(f)
    split = "holdout" if args.holdout else "dev"
    bugs = [b for b in manifest["bugs"] if b["split"] == split and (not args.only or b["id"] == args.only)]
    bugs = bugs[:args.limit] if args.limit else bugs
    out = args.resume or os.path.join(harness.HERE, "results", "bench-" + time.strftime("%Y%m%d-%H%M%S") + ".json")
    os.makedirs(os.path.dirname(out), exist_ok=True)
    prov = harness.provenance(args)
    prov.update(manifest=harness.hash_file(os.path.join(HERE, "manifest.json")), split=split, locate=LOCATE)
    result = {"config": config, "modelDigest": "" if args.rules_only else harness.model_digest(config.partition("@")[0]), "bugs": {}}
    if args.resume:
        with open(args.resume, encoding="utf-8") as f:
            old = json.load(f)
        if old["provenance"]["binary"] != prov["binary"] or old["results"][0]["config"] != config:
            raise SystemExit("That result came from a different binary or configuration, so it can't be continued.")
        result, prov = old["results"][0], old["provenance"]
    for n, bug in enumerate(bugs, 1):
        if bug["id"] in result["bugs"] and "error" not in result["bugs"][bug["id"]]:
            continue
        print(f"[{n}/{len(bugs)}] {bug['id']}", flush=True)
        try:
            result["bugs"][bug["id"]] = run_bug(bug, config, args, manifest)
        except (subprocess.CalledProcessError, RuntimeError, OSError, KeyError) as e:
            detail = getattr(e, "stderr", "") or str(e)
            result["bugs"][bug["id"]] = {"found": False, "error": str(detail)[:400]}
            print("  could not run: " + str(detail)[:160].replace("\n", " "), flush=True)
        with open(out, "w", encoding="utf-8") as f:
            json.dump({"provenance": prov, "results": [result]}, f, indent=1)
    summary(result, bugs)
    bad = [k for k, v in result["bugs"].items() if v.get("status") not in ("done", None)]
    if not args.rules_only and not any(v.get("calls") and any(c["status"] == "done" for c in v["calls"]) for v in result["bugs"].values()):
        bad.append("the model was never called successfully")
    print("\nDetails: " + os.path.relpath(out))
    if bad:
        print("NOT A VALID SCORE: " + ", ".join(bad)[:400])
        sys.exit(1)


if __name__ == "__main__":
    main()
