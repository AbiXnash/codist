# remaster vs remaster-perf

Both branches run the same original interface on the new Go backend. `remaster-perf` changes how the backend works
through a repository the first time Codist sees it, and how much data it sends to the page. Measured on 2026-11-06 on
Orbit Commerce (about 240 commits, two Spring services) from an empty Codist data folder, on a 16 GB Mac with
`qwen2.5-coder:14b` running in Ollama.

## What changed

| | remaster | remaster-perf |
|---|---|---|
| First run | Reviews only commits as they arrive | Checks the whole code once (the "baseline"), crediting each issue to the commit that last changed its line, then works through the history |
| Review order | One queue: code checks wait behind the model | Two lanes: code checks (milliseconds per commit) never wait for the model; the model reads every commit in the background, newest first |
| Same commit in both lanes | Code checks block until the model finishes it | The check lane skips a commit the model is reading, since the model's review runs the checks too |
| Restart | Starts over | Picks up the backlog where it stopped |
| While the model catches up | Every commit shows "queued" | A commit shows its code-check results, with a note that the model will read it later |
| Answers to the page | Plain JSON | Compressed (gzip), except the live event stream |
| Graph | Not the same as the first Codist | The same as the first Codist: 427 of 427 classes, 1,064 links and 2,865 method calls match the old server, including Feign links |
| History setting | None | `history` in `config.lua` or `CODIST_HISTORY` limits how many commits back the model reads |

## Measured

| | remaster | remaster-perf |
|---|---|---|
| Issues visible after start | 4, after about a minute | 9, after 3 to 4 seconds (baseline) |
| 29 commits checked by rules only | Waited 59 s behind the model | Whole history (about 240 commits) checked in about 21 s |
| Graph answer | 3.7 MB | 360 KB |
| Method calls answer | 7.2 MB | 474 KB (first build 2.8 s, then cached) |
| Repository page answer | 341 KB | 27 KB |
| Commit list answer | 96 KB | 18 KB |
| Server start / first index | 1.2 s / 0.97 s | about the same |

The model's own speed is unchanged: it still reads one commit at a time, about a minute each on this Mac, so the
full model backlog (442 commits queued on this repository) takes hours. Code-check results are what make the first
minutes useful.

## Tests

`remaster-perf` also adds tests for every package against a small two-service Spring repository built as a real git
repository (`internal/testrepo`). Statement coverage went from 38.6% to 94.5%:

| package | coverage |
|---|---|
| main | 84.6% |
| app (the HTTP API, both review lanes, branches, incidents) | 94.1% |
| config | 86.6% |
| git | 97.1% |
| incident | 96.2% |
| java (parser, checks, graph, call graph) | 94.0% |
| model | 100% |
| review | 97.3% |
| store | 98.9% |
| web | 100% |

All pass with the race detector. What is not covered: the 30-second model-server re-check, opening a browser, and
database errors SQLite cannot be made to produce.

Run them with `go test -tags 'grammar_subset grammar_subset_java' ./...`.

## Found along the way

- Deleting an incident that did not exist answered "deleted". It now answers 404.
- The incident page locates only the frames of the innermost "Caused by" exception. That is deliberate (it is
  usually where things went wrong), but when the innermost cause has only library frames, such as Hibernate's, the
  page shows no place in the project's code. Using the outer exception's project frames as a fallback is worth doing.
