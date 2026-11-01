# Issues

## Types

Each issue is classified by **type** (Bug, Security, Optimization, Better log, Readability, Dead code, Poor coding,
Test), **urgency** (Hotfix, Fix soon, Quick fix, Cleanup) and **change** (Minor, Major). All three are filters.

## Workflow

Open → In progress (assigned to you) → In review → Closed. **Mark fixed** closes an issue, and it reopens if a later
review reports it again. **Won't fix** ignores it and takes an optional reason, which goes into Memory.

The Fixed tab is all green, grouped by level: the stronger the green, the bigger the issue that was fixed.

## Issues across commits

Every finding is tracked as an **issue**, identified by its rule and its line of code (not its line number), so
the same problem is recognised in every commit and wherever the line moves.

- **Open** while the flagged code still exists in the working tree and, for rule findings, the rule still flags it;
  **resolved** once that stops (Codist records which commit fixed it, even when the fix was in another file, such
  as a constant given the right value), **ignored** if you ignored it.
- A commit's review lists only the issues it **introduced**; issues still present from earlier commits are under
  "From earlier commits". Commit badges count new issues; `✓N` shows how many earlier issues the commit fixed.
- The **Issues** panel shows every issue once, grouped by root cause, with filters by scope, state, level and
  text, and Fix / Ignore on each.
- Findings with the same root cause (the same rule in several files, or a constant and the classes using it) are
  grouped into one issue, counted once and fixed in one commit.

## Ignoring findings

From a finding's **Ignore** menu:

| Scope | Effect | Stored in |
|---|---|---|
| In this commit | Hidden for this commit only | `~/.codist/<repo>/ignores.json` |
| On this line, always | Hidden wherever the same line of code appears, in any later commit | personal, or the team file |
| This rule everywhere | The rule id is ignored for the whole repo (optionally under paths) | personal, or the team file |

Tick **Share with the team** to write the ignore to `.codist/ignore.yaml` in the repository, then commit it
like a `.gitignore`. Ignored findings do not count in badges, and the model is told not to report them.

```yaml
# .codist/ignore.yaml
- rule: console-output
  paths: ['src/main/java/com/acme/legacy/']
  reason: legacy module, replaced next quarter
- rule: copy-paste-wrong-constant
  path: src/main/java/com/acme/product/ProductService.java
  code: 'log.info(DataConstant.LOG_PREFIX + "created");'
```

## Teams, merges and reverts

- Merge commits are reviewed only for what they add themselves (lines that differ from every parent, where
  conflict resolutions live); merged-in changes are reviewed and credited in their own commits.
- An issue whose code comes back after it was fixed (a revert, a bad merge) is marked **reopened**, with who
  fixed it and who brought it back.
- During a merge, rebase, cherry-pick or revert, fixes are paused and a banner lists conflicted files;
  leftover conflict markers are critical findings.
- A pull reviews up to 30 new commits at once.
