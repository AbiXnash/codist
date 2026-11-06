# Behaviour-difference experiment, 2026-11-06

Question: if Codist runs the methods a commit changed, on the code before and after, with the same inputs, how many
real bugs show a difference? Same 10 benchmark bugs as the earlier re-scoring sample; each "commit" is the bug's
fix, so the code before is buggy and the code after is fixed.

## How

1. `changed` (Go, on Codist's own Java parser and call graph) lists the methods the fix changed, plus the project
   methods that call them.
2. Both versions are compiled from source with their Maven dependencies (`compile.sh`). All 10 compile.
3. `harness/Harness.java`, written once and never by the model, loads each version in its own class loader and calls
   every listed method with the same inputs. It compares the return value, the exception type, the arguments after
   the call (text written to an `Appendable`, for one) and the object's fields that exist in both versions. The buggy
   version also runs against itself, and inputs whose result changes between runs are not counted.
   - **A**: standard edge cases per type, one parameter at a time, plus seeded mixes (up to 300 calls per method).
     Objects are built from the class's constants, constructors, factory methods or concrete subclasses.
   - **B**: the local model (`qwen2.5-coder:14b`) reads the commit and suggests plain JSON values; the harness checks
     each value against the parameter's type and fills parameters it left out with A's values.

## Results

| bug | A (edge cases) | B (model's values) | what differs |
|---|---|---|---|
| Csv-4 | yes | yes | buggy `getHeaderMap()` throws NullPointerException |
| Csv-15 | yes | yes | buggy version quotes non-ASCII text such as `ÿé中` |
| solarpositioning | yes | yes | ΔT estimates differ for 2010 to 2100 and later years |
| ensembler | yes | yes | button text differs |
| epubcheck | no | yes | `--help` and `--version`: buggy `processArguments` returns false, fixed true |
| crawler-commons | no | no | the model's robots.txt had no `User-agent` line, so both ignore it |
| Cli-4 | no | no | needs `Options` holding a required option: a built-up object |
| JxPath-16 | no | no | needs DOM nodes |
| jsoup | no | no | needs a parsed document |
| ByteSkript | no | no | compiles scripts; results change between runs, so nothing can be compared |
| **total** | **4 of 10** | **5 of 10** | |

False alarms: none in the final run. One appeared earlier (epubcheck: the fix added a field, so the object always
looked different) and is fixed by comparing only fields both versions have. Differences between runs were caught by
the control run (215 inputs in ByteSkript) and left out.

Cost: A runs in seconds to a few minutes per bug. B adds one model call per commit, 12 to 77 seconds here.

## What it means

The agreed bar was about 3 of 10 with few false alarms. A alone gives 4 and A with B gives 5, with no false alarms, so
"show the behaviour difference and ask if it was meant" passes as a direction.

Not yet measured, and needed before relying on it:

- False alarms on commits that should not change behaviour (refactors, renames). Here every commit was a fix, so a
  difference was always expected. Run the same harness on refactoring commits and count differences.
- The 5 planted Orbit Commerce bugs, which are Spring code with repositories and Feign clients (the agreed Mockito fakes).
- The misses are all built-up objects. Letting the model describe one as data (for example "Options with a required
  option a" as a list of setter calls the harness performs) would target Cli-4, JxPath and jsoup without the model
  writing code.

## Files

Everything is in `eval/behaviour/`. `setup.sh [list]` checks out and compiles each commit and its parent and lists the
changed methods (`changed/`, on Codist's Java parser); `suggest.py` asks the model for inputs (`LIST=file` for
another list); `run.sh A|B [list]` runs `harness/Harness.java`. Per bug: `methods.txt`, `inputs.json`, `suggest.log`;
reports in `reports/<bug>.<A|B>.txt`.
