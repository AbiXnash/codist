#!/usr/bin/env python3
"""Measures Codist's reviewer against known bugs, so changes to models and prompts can be compared.

  python3 eval/run.py                                     the configured model
  python3 eval/run.py qwen2.5-coder:7b qwen2.5-coder:3b   compare models
  python3 eval/run.py qwen2.5-coder:7b@8192               with a context size
  python3 eval/run.py --rules-only                        without any model
  python3 eval/run.py MODEL --no-rules --no-filters       what the model finds alone
  python3 eval/run.py MODEL --no-rag                      what retrieval earns; also --no-facts
  python3 eval/run.py MODEL --repeat 3                    run each configuration three times
  python3 eval/run.py MODEL --default-settings            the settings users get, not the eval's
  python3 eval/run.py --compare A.json B.json             paired test between two result files

Needs server/bin/codist (make build) and, unless --rules-only, Ollama. It works on a temporary copy
and changes nothing in this repository. Results are written to eval/results/<time>.json with what is
needed to reproduce them. A model run in which the model was never called exits with an error, so an
outage cannot pass for a score.
"""
import argparse, hashlib, json, math, os, platform, re, shutil, socket, subprocess, sys, tempfile, time, urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
JAVA = "src/main/java/com/demo/library/"
RANK = ["info", "low", "medium", "high", "critical"]
SWITCHES = {"no_rules": "CODIST_NO_RULES", "no_rag": "CODIST_NO_RAG", "no_facts": "CODIST_NO_FACTS", "no_filters": "CODIST_NO_FILTERS"}
DROPPED = "Dropped a model finding: "


def binary():
    if os.environ.get("CODIST_BIN"):
        return os.environ["CODIST_BIN"]
    return os.path.join(ROOT, "server", "bin", "codist.exe" if os.name == "nt" else "codist")


def git(repo, *args):
    subprocess.run(["git", "-C", repo, "-c", "user.name=Eval", "-c", "user.email=eval@codist", "-c", "core.autocrlf=false", *args], check=True, capture_output=True)


def get(port, path, data=None):
    req = urllib.request.Request(f"http://localhost:{port}/api{path}", data=None if data is None else json.dumps(data).encode())
    with urllib.request.urlopen(req, timeout=30) as r:
        return json.load(r)


def free_port():
    with socket.socket() as s:
        s.bind(("", 0))
        return s.getsockname()[1]


def wilson(found, total, z=1.96):
    """The 95% confidence range of a share, as (low, high). Small test sets give wide ranges."""
    if total == 0:
        return (0.0, 1.0)
    p = found / total
    denom = 1 + z * z / total
    mid = (p + z * z / (2 * total)) / denom
    half = z * math.sqrt(p * (1 - p) / total + z * z / (4 * total * total)) / denom
    return (max(0.0, mid - half), min(1.0, mid + half))


def mcnemar(wins, losses):
    """Exact two-sided paired test: how likely is a split this uneven if neither side is better?"""
    n = wins + losses
    if n == 0:
        return 1.0
    tail = sum(math.comb(n, i) for i in range(min(wins, losses) + 1)) / 2 ** n
    return min(1.0, 2 * tail)


def score(findings, labels, repo):
    """Which labels were found, which findings match nothing, and which only the alsoTrue list excused."""
    used = set()
    results = {}
    for bug in labels["bugs"]:
        path = JAVA + bug["file"]
        with open(os.path.join(repo, path), encoding="utf-8") as f:
            text = f.read().split("\n")
        at = next((i + 1 for i, l in enumerate(text) if bug["near"] in l), None)
        if at is None:
            raise SystemExit(f"label {bug['id']}: {bug['near']!r} is not in {path}")
        pattern = re.compile(bug["match"], re.I)
        hits = [i for i, f in enumerate(findings) if f["path"] == path and abs(f["line"] - at) <= 5 and pattern.search(f"{f['title']} {f.get('detail', '')}")]
        results[bug["id"]] = {"kind": bug["kind"], "found": bool(hits), "by": sorted({findings[i].get("source", "") for i in hits})}
        used.update(hits)
    also = re.compile(labels["alsoTrue"], re.I)
    unmatched = [f for i, f in enumerate(findings) if i not in used]
    noise = [f for f in unmatched if not also.search(f["title"])]
    excused = [f for f in unmatched if also.search(f["title"])]
    clean_paths = {JAVA + c for c in labels["clean"]}
    false_alarms = [f for f in findings if f["path"] in clean_paths and RANK.index(f["severity"]) >= RANK.index("low")]
    return results, noise, excused, false_alarms


def model_calls(activity):
    """The real model calls in an activity list, with the timing of each."""
    return [{"kind": a["kind"], "path": a.get("path", ""), "status": a.get("status", ""), "tookMs": a.get("tookMs", 0),
             "promptTokens": a.get("promptTokens") or 0, "replyTokens": a.get("replyTokens") or 0}
            for a in activity if a.get("model") and a["kind"] != "step"]


def drops(activity):
    """Every model finding the filters removed, with the reason."""
    return [{"reason": a["title"][len(DROPPED):], "title": a.get("note", ""), "path": a.get("path", "")}
            for a in activity if a.get("title", "").startswith(DROPPED)]


def hash_tree(path):
    h = hashlib.sha256()
    for base, dirs, files in os.walk(path):
        dirs.sort()
        for name in sorted(files):
            full = os.path.join(base, name)
            h.update(os.path.relpath(full, path).replace(os.sep, "/").encode())
            with open(full, "rb") as f:
                h.update(f.read())
    return h.hexdigest()[:16]


def hash_file(path):
    with open(path, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()[:16]


def memory_gb():
    try:
        if sys.platform == "darwin":
            return round(int(subprocess.check_output(["sysctl", "-n", "hw.memsize"], text=True)) / 2 ** 30)
        if os.name == "nt":
            out = subprocess.check_output(["powershell", "-NoProfile", "-Command", "(Get-CimInstance Win32_ComputerSystem).TotalPhysicalMemory"], text=True)
            return round(int(out.strip()) / 2 ** 30)
        return round(os.sysconf("SC_PAGE_SIZE") * os.sysconf("SC_PHYS_PAGES") / 2 ** 30)
    except (OSError, ValueError, subprocess.SubprocessError, AttributeError):
        return None


def ollama(path, data=None):
    url = os.environ.get("OLLAMA_URL", "http://localhost:11434") + path
    try:
        req = urllib.request.Request(url, data=None if data is None else json.dumps(data).encode())
        with urllib.request.urlopen(req, timeout=5) as r:
            return json.load(r)
    except (OSError, ValueError):
        return {}


def model_digest(model):
    for m in ollama("/api/tags").get("models", []):
        if m.get("name") == model:
            return m.get("digest", "")[:16]
    return ""


def provenance(args):
    """What is needed to reproduce a run, and to know when two results are not comparable."""
    head = lambda *a: subprocess.run(["git", "-C", ROOT, *a], capture_output=True, text=True).stdout.strip()
    return {
        "commit": head("rev-parse", "HEAD"), "dirty": bool(head("status", "--porcelain", "--", "server", "eval")),
        "binary": hash_file(binary()), "prompts": hash_tree(os.path.join(ROOT, "server", "prompts")),
        "labels": hash_file(os.path.join(HERE, "labels.json")), "project": hash_tree(os.path.join(HERE, "project")),
        "ollama": ollama("/api/version").get("version", ""), "os": platform.system(), "machine": platform.machine(), "memoryGb": memory_gb(),
        "settings": {"defaultSettings": args.default_settings, **{k: getattr(args, k) for k in SWITCHES}},
        "at": time.strftime("%Y-%m-%dT%H:%M:%S%z"),
    }


def run(config, labels, args):
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
    env = dict(os.environ, PORT=str(port), CODIST_DATA_DIR=data, CODIST_AUTO_REVIEW="false", CODIST_NO_BROWSER="1")
    if not args.default_settings:
        env.update(CODIST_TOOLS="off", CODIST_MAX_FILES="100")
    for name, var in SWITCHES.items():
        if getattr(args, name):
            env[var] = "true"
    if rules_only:
        env["CODIST_MODEL_REVIEW"] = "false"
    else:
        env["CODIST_MODEL"] = model
    if ctx:
        env["CODIST_NUM_CTX"] = ctx
    proc = subprocess.Popen([binary(), repo], env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    try:
        for _ in range(100):
            try:
                get(port, "/status")
                break
            except OSError:
                time.sleep(0.2)
        start = time.time()
        get(port, f"/commits/{sha}/review", {})
        while True:
            time.sleep(0.5 if rules_only else 2)
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
        activity = get(port, "/activity")
        calls = model_calls(activity)
        results, noise, excused, false_alarms = score(findings, labels, repo)
        line = lambda f: f"{f['severity']} [{f.get('source', '')}] {f['title']} ({os.path.basename(f['path'])}:{f['line']})"
        return {"config": config, "seconds": round(took), "status": review.get("status"), "modelDigest": "" if rules_only else model_digest(model),
                "calls": calls, "tokensIn": sum(c["promptTokens"] for c in calls), "tokensOut": sum(c["replyTokens"] for c in calls),
                "findings": len(findings), "bugs": results, "drops": drops(activity), "all": [line(f) for f in findings],
                "noise": [line(f) for f in noise], "excused": [line(f) for f in excused], "falseAlarms": [line(f) for f in false_alarms],
                "skipped": [f["path"] for f in review.get("files") or [] if f.get("skipped")],
                "failedFiles": [f["path"] for f in review.get("files") or [] if f.get("error")]}
    finally:
        proc.terminate()
        proc.wait()
        shutil.rmtree(work, ignore_errors=True)


def problems(result, rules_only):
    """Reasons a result must not be treated as a score."""
    out = []
    if not rules_only and not [c for c in result["calls"] if c["status"] == "done"]:
        out.append(f"{result['config']}: the model was never called successfully")
    if result["failedFiles"]:
        out.append(f"{result['config']}: {len(result['failedFiles'])} file(s) failed")
    return out


def percent(found, total):
    low, high = wilson(found, total)
    return f"{found}/{total} ({low * 100:.0f}-{high * 100:.0f}%)"


def report(results):
    print("\n%-28s %-16s %-14s %5s %6s %5s %5s %5s %6s %6s  %s" % ("config", "found", "logic", "noise", "excuse", "false", "drops", "calls", "secs", "s/call", "missed"))
    for r in results:
        found = sum(v["found"] for v in r["bugs"].values())
        logic = [v for v in r["bugs"].values() if v["kind"] == "logic"]
        missed = [k for k, v in r["bugs"].items() if not v["found"]]
        times = [c["tookMs"] for c in r["calls"]]
        each = f"{sum(times) / len(times) / 1000:.1f}" if times else "-"
        print("%-28s %-16s %-14s %5d %6d %5d %5d %5d %6d %6s  %s" % (r["config"], percent(found, len(r["bugs"])), percent(sum(v["found"] for v in logic), len(logic)), len(r["noise"]), len(r["excused"]), len(r["falseAlarms"]), len(r["drops"]), len(r["calls"]), r["seconds"], each, ", ".join(missed)))
    reasons = {}
    for r in results:
        for d in r["drops"]:
            key = re.sub(r"rule \S+", "rule", d["reason"])
            reasons[key] = reasons.get(key, 0) + 1
    if reasons:
        print("\nDropped: " + ", ".join(f"{v} {k}" for k, v in sorted(reasons.items(), key=lambda x: -x[1])))


def compare(a_path, b_path):
    """Paired comparison of the bugs each file's last run found. Returns the numbers it printed."""
    def last(path):
        with open(path, encoding="utf-8") as f:
            data = json.load(f)
        return (data.get("provenance") or {}), data["results"][-1] if isinstance(data, dict) else data[-1]
    (pa, a), (pb, b) = last(a_path), last(b_path)
    if pa.get("labels") != pb.get("labels"):
        print("Warning: these used different labels, so the comparison is not fair.")
    wins = sorted(k for k in a["bugs"] if b["bugs"].get(k, {}).get("found") and not a["bugs"][k]["found"])
    losses = sorted(k for k in a["bugs"] if a["bugs"][k]["found"] and not b["bugs"].get(k, {}).get("found"))
    p = mcnemar(len(wins), len(losses))
    print(f"{a['config']} -> {b['config']}: {len(wins)} gained, {len(losses)} lost, p = {p:.3f}")
    if wins:
        print("  gained: " + ", ".join(wins))
    if losses:
        print("  lost:   " + ", ".join(losses))
    print("  " + ("Significant at p < .05." if p < 0.05 else "Not significant: treat this as no difference."))
    return len(wins), len(losses), p


def main():
    ap = argparse.ArgumentParser(description="Measure Codist against known bugs.")
    ap.add_argument("configs", nargs="*", help="models, optionally MODEL@CONTEXT")
    ap.add_argument("--rules-only", action="store_true", help="run without any model")
    ap.add_argument("--no-rules", action="store_true", help="no rule findings and no rule hints")
    ap.add_argument("--no-rag", action="store_true", help="no retrieved bug patterns")
    ap.add_argument("--no-facts", action="store_true", help="no related types, reach or cost facts")
    ap.add_argument("--no-filters", action="store_true", help="keep every model finding with a valid position")
    ap.add_argument("--default-settings", action="store_true", help="use the settings users get, including the file limit")
    ap.add_argument("--repeat", type=int, default=1, help="runs per configuration")
    ap.add_argument("--compare", nargs=2, metavar="FILE", help="compare two result files")
    args = ap.parse_args()
    if args.compare:
        compare(*args.compare)
        return
    if not os.path.exists(binary()):
        raise SystemExit(f"{binary()} is missing; run make build")
    with open(os.path.join(HERE, "labels.json"), encoding="utf-8") as f:
        labels = json.load(f)
    configs = (["--rules-only"] if args.rules_only else []) + (args.configs if args.configs or args.rules_only else [os.environ.get("CODIST_MODEL", "qwen2.5-coder:14b")])
    prov = provenance(args)
    results, bad = [], []
    for c in configs:
        for n in range(args.repeat):
            print(f"running {c}" + (f" (run {n + 1} of {args.repeat})" if args.repeat > 1 else "") + " ...", flush=True)
            r = run(c, labels, args)
            results.append(r)
            bad += problems(r, c == "--rules-only")
    report(results)
    out = os.path.join(HERE, "results")
    os.makedirs(out, exist_ok=True)
    path = os.path.join(out, time.strftime("%Y%m%d-%H%M%S") + ".json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump({"provenance": prov, "valid": not bad, "problems": bad, "results": results}, f, indent=2)
    print("\nDetails: " + os.path.relpath(path))
    if prov["dirty"]:
        print("Note: server or eval had uncommitted changes, so this result can't be reproduced from a commit.")
    if bad:
        print("\nNOT A VALID SCORE:\n  " + "\n  ".join(bad))
        sys.exit(1)


if __name__ == "__main__":
    main()
