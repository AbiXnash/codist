#!/usr/bin/env python3
"""Score a saved bench run against hand labels, with no model run.

"located" only says a finding landed on a changed line. labels-14b.json records whether a person reading
the finding's title beside the real fix thought it named the bug: real, related or no.
Usage: score_labels.py RESULTS.json MODEL
"""
import json, os, re, sys

PAT = re.compile(r"^(\w+) \[(\w+)\] (.*) \((.*):(\d+)\)$")
NULL = re.compile(r"null|npe", re.I)
CHANGE = re.compile(r"^(improved|simplified|added|adding|renamed|refactored|updated|changed|moved|replaced|introduced|extracted|explicitly passing|use of)\b", re.I)
HERE = os.path.dirname(os.path.abspath(__file__))


def main(path, model):
    labels = json.load(open(os.path.join(HERE, "labels-14b.json"), encoding="utf-8"))["labels"]
    res = json.load(open(path, encoding="utf-8"))["results"]
    res = res if isinstance(res, list) else list(res.values())
    bugs = [r for r in res if r["config"].endswith(model)][0]["bugs"]
    rows = []
    for bug, v in bugs.items():
        if v.get("kind") != "real" or not v["found"]:
            continue
        rows.append((bug, labels.get(bug, "?"), [PAT.match(h) for h in v["hits"] if PAT.match(h)]))
    count = lambda kept: {k: sum(1 for _, l, _ in kept if l == k) for k in ("real", "related", "no", "?")}
    total = len([v for v in bugs.values() if v.get("kind") == "real"])
    print(f"{len(rows)} of {total} located:", count(rows))
    groups = {
        "only low or info": lambda h: all(m[1] in ("low", "info") for m in h),
        "only rule findings": lambda h: all(m[2] == "rule" for m in h),
        "only null claims": lambda h: all(NULL.search(m[3]) for m in h),
        "only the copied null title": lambda h: all(m[3] == "Null dereference on some path" for m in h),
        "only change descriptions": lambda h: all(m[2] == "llm" and CHANGE.search(m[3]) for m in h),
    }
    for name, test in groups.items():
        print(f"  {name}:", count([r for r in rows if test(r[2])]))
    alarms = [PAT.match(a) for v in bugs.values() if v.get("kind") == "real" for a in v.get("falseAlarms", []) if PAT.match(a)]
    print("  false alarms:", len(alarms), "low or info", sum(m[1] in ("low", "info") for m in alarms),
          "copied null title", sum(m[3] == "Null dereference on some path" for m in alarms),
          "change descriptions", sum(m[2] == "llm" and bool(CHANGE.search(m[3])) for m in alarms))
    fixes = lambda test: sum(1 for v in bugs.values() if v.get("kind") == "real" and any(test(PAT.match(a)) for a in v.get("falseAlarms", []) if PAT.match(a)))
    weak = lambda m: m[1] in ("low", "info") or m[3] == "Null dereference on some path" or (m[2] == "llm" and CHANGE.search(m[3]))
    print("  fixes with an alarm:", fixes(lambda m: True), "after moving weak findings to notes:", fixes(lambda m: not weak(m)))
    kept = [r for r in rows if not all(weak(m) for m in r[2])]
    print("  located after moving weak findings to notes:", len(kept), count(kept))


if __name__ == "__main__":
    main(sys.argv[1], sys.argv[2])
