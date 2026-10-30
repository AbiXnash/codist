# Codist

A local code reviewer for Java / Spring Boot repositories. It watches a git repo, draws a live graph of
how your classes link together, and reviews every new commit with deterministic Spring/Java checks plus
a local LLM (Ollama). Nothing leaves your machine.

```
server/   Go: git watcher, tree-sitter Java indexer, rule checks, Ollama reviewer, REST + SSE API
web/      Svelte 5 + Vite: light/dark UI with Cytoscape graph and Shiki code view
```

## Requirements

| Requirement | Why | Notes |
|---|---|---|
| **Go 1.22+** | the server | `server/go.mod` targets Go 1.22. Go 1.23+ works. |
| **A C compiler** | building the server | The tree-sitter Java parser uses cgo. Install Xcode Command Line Tools on macOS (`xcode-select --install`), `build-essential` on Debian/Ubuntu, or Xcode/Clang on Windows. `CGO_ENABLED=0` will **not** build. |
| **Node.js 20.19+ or 22.12+** | the web UI | Required by Vite 8. npm is included with Node. |
| **git** | everything | Codist uses the real git history, diffs and `git blame`. |
| **Ollama** | model features | Needed for AI review, fixes, Ask and incident explanations. See [Models](#models). Without it, deterministic checks still work. |
| **Java 21 + Maven** | optional | Only for PMD and SpotBugs findings on Maven projects. Maven downloads the analysers on first use. |
| **~8 GB RAM** | local model | A 7B model needs roughly 5–6 GB while loaded. 16 GB is comfortable; 32 GB+ allows 14B/32B models. |

Linux, macOS (Apple Silicon and Intel) and Windows (WSL recommended) work.

## Models

Codist talks to a local [Ollama](https://ollama.com) server. The default is
**`qwen2.5-coder:7b`**, a good balance of review quality, speed and memory on a laptop.

```bash
ollama pull qwen2.5-coder:7b
```

Use any Ollama model that follows instructions and returns JSON reasonably well — Codist asks for
strict JSON and retries once on malformed output, but small or chat-only models produce weaker findings.
The model gets a 16k-token context by default (`CODIST_NUM_CTX`).

Different jobs can use different models, which is useful on machines that can only keep one model in memory:

| Env var | Used for | Suggested |
|---|---|---|
| `CODIST_MODEL` | file review and analysis | `qwen2.5-coder:7b` (default) |
| `CODIST_SUMMARY_MODEL` | commit summaries | a smaller model, e.g. `qwen2.5-coder:3b` |
| `CODIST_FIX_MODEL` | suggested fixes and code changes | a stronger model if you have RAM, e.g. `qwen2.5-coder:14b` |
| `CODIST_ASK_MODEL` | Ask tab and incident root-cause explanations | same as `CODIST_FIX_MODEL` |

Codist keeps the model loaded for 30 minutes (`keep_alive`), caches answers, and runs files one at a
time. See [Speed](#speed) for memory tuning.

## Alternatives

**Another Qwen2.5-Coder size.** `qwen2.5-coder:3b` on a low-memory laptop, or `:14b` / `:32b` when you
have the RAM. Larger models find more, but reviews take longer.

**Another Ollama model.** Coder or instruct models such as DeepSeek-Coder-V2, Granite Code or CodeLlama
can be selected with `CODIST_MODEL`. JSON reliability varies; expect to experiment and check the
review for malformed-output retries.

**A remote or shared Ollama.** Point `OLLAMA_URL` at another machine, for example a GPU box:

```bash
OLLAMA_URL=http://gpu-box:11434 CODIST_MODEL=qwen2.5-coder:32b make run REPO=~/dev/app
```

**Another runtime.** Codist uses Ollama's HTTP API (`/api/chat` and `/api/tags`), so any server that
exposes an Ollama-compatible API works — set `OLLAMA_URL` to it. A runtime that only offers the OpenAI
API needs an Ollama-compatible proxy in front of it. No MCP server or cloud account is involved.

**No model at all.** Codist still watches the repository, draws the code graph, runs the deterministic
rules, checks uncommitted changes and (for Maven projects) PMD/SpotBugs. AI review, fixes, Ask and
incident explanations are skipped, and the UI says why. This is a fully offline, zero-model mode.

**No static analysers.** Set `CODIST_TOOLS=off` to skip PMD and SpotBugs even when Maven is available.

**A fine-tuned model.** See [Fine-tuning](#fine-tuning-optional); point `CODIST_MODEL` at the model you
create with `ollama create`.

## Run

### Quick start (local checkout)

```bash
git clone https://github.com/AbiXnash/codist.git
cd codist

make install                        # npm install + go mod download
ollama pull qwen2.5-coder:7b        # in another terminal if Ollama is not running
make run REPO=~/dev/your-spring-app # builds the UI and server, then serves http://localhost:8080
```

`make run` builds `web/dist`, compiles `server/bin/codist`, and starts it. Stop it with `Ctrl+C`.
You need an existing Java/Spring Boot git repository to point `REPO` at; Codist does not create one.

### Development (hot reload)

```bash
make dev REPO=~/dev/your-spring-app
# UI: http://localhost:5173, API: http://localhost:8080
```

### Follow a GitHub repository

Instead of a local checkout, let Codist clone and follow `origin/<branch>`:

```bash
cd server
CODIST_REPO_URL=https://github.com/you/app.git \
GITHUB_TOKEN=ghp_... \
go run .
```

The clone is kept in `~/.codist/repos`. For public repositories `GITHUB_TOKEN` is optional.

### Build once and run the binary

```bash
make build
cd server
CODIST_REPO=~/dev/your-spring-app ./bin/codist
```

### Change the port

```bash
PORT=9090 make run REPO=~/dev/your-spring-app
```

### Test

```bash
make test      # Go unit tests + svelte-check
```

### Troubleshooting

- **`undefined: Node`** while building — cgo is disabled or no C compiler is installed. Install one and
  run `go env -w CGO_ENABLED=1`.
- **"Model … is not pulled"** in the UI — run `ollama pull qwen2.5-coder:7b`, or set `CODIST_MODEL` to a
  model you already have (`ollama list`).
- **Ollama is not reachable** — start it with `ollama serve`, or set `OLLAMA_URL` if it runs elsewhere.
- **Out of memory with several models** — set `OLLAMA_MAX_LOADED_MODELS=1` so Ollama swaps models
  instead of loading them all.

## Configuration

All settings are environment variables:

| Env var | Default | Meaning |
|---|---|---|
| `CODIST_REPO` | – | Local git checkout to watch |
| `CODIST_REPO_URL` | – | Git URL to clone into `~/.codist/repos` and follow (`origin/<branch>`) |
| `CODIST_BRANCH` | current branch | Branch to follow |
| `CODIST_FETCH_REMOTE` | `false` | With `CODIST_REPO`: follow `origin/<branch>` instead of the local branch |
| `CODIST_MODEL` | `qwen2.5-coder:7b` | Ollama model for review |
| `CODIST_SUMMARY_MODEL` | same as `CODIST_MODEL` | Optional model for commit summaries |
| `CODIST_FIX_MODEL` | same as `CODIST_MODEL` | Optional model for fixes and code changes |
| `CODIST_ASK_MODEL` | same as `CODIST_MODEL` | Optional model for Ask and incident explanations |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama-compatible endpoint |
| `CODIST_NUM_CTX` | `16384` | Context window requested from the model |
| `CODIST_DATA_DIR` | `~/.codist` | Reviews, caches, prompts, incidents and ignores |
| `CODIST_TOOLS` | `auto` | `auto`, `off`, or a list of static analysers |
| `CODIST_MAX_FILES` | `8` | Maximum files reviewed per commit |
| `CODIST_AUTO_REVIEW` | `true` | Review new commits automatically |
| `CODIST_POLL_MS` | `3000` | How often the watcher checks for changes |
| `PORT` | `8080` | HTTP port |
| `CODIST_ADDR` | `:$PORT` | Full listen address, overrides `PORT` |
| `CODIST_WEB_DIR` | `../web/dist` | Static UI directory |
| `CODIST_RULES_DIR` | – | Extra knowledge-base folder |
| `GITHUB_TOKEN` | – | For private repositories |
| `CODIST_WEBHOOK_SECRET` | – | Verifies GitHub webhook signatures |

Reviews are stored as JSON in `~/.codist/<repo>/reviews/`.

## Layout

- **History** (left): your commits by default (who "you" are comes from `git config user.name`), or everyone's.
  Your totals sit on top: commits, open issues your commits introduced, issues you fixed. Each commit shows its
  new issues by level, what it fixed, and whether it is still being reviewed.
- **Graph** (centre): the selected commit's changed, added and impacted types, and a ring on every type that has
  an issue in the list on the right, in its worst severity. **Focus** dims everything else.
- **Issues** (right): every issue in one place. Choose the scope (all code, this commit, mine), the state (open,
  fixed, ignored) and a level (critical, high, medium, low); the list is grouped by level. Open an issue for its
  places, suggested fix, history (who introduced and fixed it), and Fix / Ignore. The graph jumps to it. The
  Commit, Diff, Code and Ask tabs sit next to it.

## Uncommitted changes

While you edit (local checkout), Codist runs the rules and the knowledge base on the lines you changed but have
not committed, in milliseconds and without the model. They appear at the top of History as **Uncommitted
changes** and in the Issues hub's **Uncommitted** scope; the graph marks the types you are editing. Fixes there
are applied to the working tree and **not committed**, so you commit them together with your own change. Issues
your edit already fixes show as "fixed locally".

Some findings are heads-ups rather than defects, such as "shared constant changed": they have **Mark as checked**
instead of Fix, because a fix could only undo your change.

### The graph

- **Controls** (top right of the graph): find by class, package or `@Annotation` (highlight or show only
  matches); show or hide roles, link kinds, tests and packages; layouts **Natural**, **Layers**
  (controllers on top, then services, repositories, entities), **Hubs** and **Grid**; size by links, code
  size or open issues; labels; package boxes; PNG export.
- **Right-click a class**: blast radius (what depends on it, by distance), show only its neighbours,
  dependencies or dependents, path from or to it, hide it, open its code.
- **Analysis**: dependency **cycles**, **layer violations** (upward dependencies, controllers using
  repositories directly), **blast radius**, **path** between two classes, and **hotspots** (most depended
  on, fragile, largest). Hover a class for fan-in, fan-out, instability and size.
- **What does it all mean?** (the ? button) explains every shape, letter, ring, line and colour.

## Keyboard

| Key | Action |
|---|---|
| `/` or `⌘K` | Go to class |
| `[` / `]` | Hide or show the history / right panel |
| `j` / `k` | Next / previous commit |
| `1`–`5` | Issues, Commit, Diff, Code, Ask tab |
| `⌘↵` | Commit the fix in the fix dialog; "change code" in the Ask tab |

## Fixing and editing from the UI

- **Fix** on a finding asks the fix model for a corrected file, checks that it still parses and has not lost
  code, and shows the diff. **Apply & commit** writes the file and commits *only that file* to the current
  branch (other uncommitted work is left alone); the new commit is reviewed automatically. Nothing is pushed.
- **Ask** answers questions about the open file or selected class, and **Change code** turns an instruction
  ("add retry with backoff to the gateway call") into the same reviewed diff.
- Fixes need a local checkout (`CODIST_REPO`), and refuse to run if the file changed since the review.

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

## Instant fixes

Fixes for these rules are mechanical, so they open immediately and never involve the model: missing injection
(adds a constructor), `System.out` (adds an SLF4J logger and imports), a literal that repeats a constant, a
duplicated log line, a log-only helper method, and a constant borrowed from another domain. Model fixes for the
other issues are prepared in the background after each review (marked ⚡), so they also open instantly. Fixes work
on the working tree as it is now: each place is found again by its code, and places already fixed are skipped.

## Commit messages

Generic messages ("chore: added logs", "update", "fix") and `chore`/`docs`/`style` commits that change production
code are flagged, with a suggested Conventional Commits message that describes what the diff actually does.

## Prompts

Every instruction Codist gives a model is a Markdown file in `server/prompts/` (per-file review, commit
summary, fixes, Ask, regression tests, incident RCA, rules, runbooks, and the feedback sent when an
answer is wrong). They are built in; to change one, `codist export-prompts` copies them to
`~/.codist/prompts/`, where edits apply immediately. A team can commit its own in `.codist/prompts/`,
which win over personal ones. `GET /api/prompts` shows which version is in effect.

## Knowledge: bugbase and retrieval (RAG)

`server/internal/knowledge/bugbase/*.md` describes about 110 real-world Java, Spring, JPA, concurrency,
security, performance and testing defects: what the pattern is, how it shows up in code, why it breaks in
production, bad and better code, and the equivalent Sonar, SpotBugs or PMD rule. They are knowledge, not
"check for X" instructions: for each file the most similar entries are retrieved (BM25 over the code's
identifiers, annotations and calls, changed lines weighted higher) and given to the model as reference;
the model reasons from them and finds problems they do not mention. YAML rules join the same index.
Add your own entries in `~/.codist/bugbase/` or `.codist/bugbase/` (same format; same id overrides).

## Static analysers

For Maven projects Codist also runs **PMD 7** (a curated rule set: defects, not style) and **SpotBugs**
(bytecode: unwritten fields, null paths, leaks, concurrency) on each reviewed commit, in an export of the
commit, so your working tree is never touched. Their findings on changed lines join Codist's own (one
problem is not listed twice) before the model runs, so it sees them as evidence. `CODIST_TOOLS=off`
disables them; Maven downloads the plugins on first use.

## Fix with a suggestion

Every open issue has **Suggest how to fix it**: describe the fix you want ("constructor injection, keep the
field name") and the fix follows it. The suggestion must be about that issue: naming another class of the
project is refused at once, and a model check refuses other requests (features, other bugs, chat) with the
reason, before any fix is generated.

## Teams, merges and reverts

- Merge commits are reviewed only for what they add themselves (lines that differ from every parent, where
  conflict resolutions live); merged-in changes are reviewed and credited in their own commits.
- An issue whose code comes back after it was fixed (a revert, a bad merge) is marked **reopened**, with who
  fixed it and who brought it back.
- During a merge, rebase, cherry-pick or revert, fixes are paused and a banner lists conflicted files;
  leftover conflict markers are critical findings.
- A pull reviews up to 30 new commits at once.

## Incidents: root cause analysis from logs

Try it on `~/dev/rca-demo` (a Spring Boot app calling an unreliable quote service; its log fills with real
NullPointerExceptions). See that folder's README.

Open **Incidents** in the top bar, paste the logs from an incident (or drop a log file) and click **Find root cause**.
The logs are analysed on this machine and never sent anywhere.

1. **Errors.** Stack traces, `Caused by` chains and ERROR lines are parsed; repeats are merged and counted.
2. **Code.** Frames in the project are mapped to classes, methods and lines through the code graph, with
   `git blame` for each line and any open Codist issue nearby.
3. **Suspect commits.** Recent commits that touched the failing code are ranked. Strongest signals: Codist flagged a
   severe issue there in that commit; the commit last changed a field the error names ("this.customers is null");
   it changed the failing method; it last changed the failing line.
4. **Cause.** The local model explains the most likely causes, each with log and code evidence and a confidence.
5. **Act.** Confirm the root cause, then: **Propose fix** (an exact tracked fix when one exists), **Regression test**
   (a test that fails while the defect exists; dependency-injection defects get an exact Spring wiring test), and
   **Create review rule**, which saves the pattern to `.codist/rules/learned.yaml` so commit review catches it next time.
6. **Report.** An editable RCA report (summary, timeline, impact, root cause, fix, prevention) to copy as Markdown,
   and a runbook draft for the support team that can be committed under `docs/runbooks/`.

Past incidents are searched for similar ones (same exceptions, frames and messages) and their confirmed causes are
shown and given to the model.

## Release risk report

**Release** judges the whole codebase at the commit you are releasing (HEAD by default, or any commit or tag),
not the commits that led to it. Every Java file is checked with the rules and the knowledge base, the model's tracked
findings are added where their code is still there, and duplicates are grouped by root cause. The report shows every
open issue with who wrote the line and in which commit, issues per module, build problems, which source files the
model has never reviewed, an overall risk with reasons, and a sign-off checklist. The checklist is one per release
commit, saved by Codist and shared live, with who ticked each item and when. Copy it all as Markdown for the release
ticket. Below it, the dependency watch lists
every dependency in the build file with known-vulnerable versions, dev-only libraries on the production classpath,
pinned versions that override Spring Boot's, and end-of-life Spring Boot lines.

Not included: ticketing (Jira, ServiceNow), monitoring alerts and pull-request comments, which need integrations.

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

## Knowledge base: teaching the model

The model is not retrained; it is *briefed*. `server/internal/knowledge/rules/*.yaml` holds 100+ senior review
rules (concurrency and lost updates, transactions, JPA/N+1, security, performance, logging, money and time,
deprecated APIs, configuration, API design, copy-paste slips, tests). Each has what to look for, why it
matters, and bad / good code.

- For each changed file Codist ranks the rules by how well their `triggers` match the changed lines and puts
  the top 10 in the prompt as a checklist. Findings that match a rule carry its id.
- Rules with `detect` patterns are deterministic: a matching changed line is reported without the model
  (deprecated APIs such as `javax.*`, `getById`, `@MockBean`, `new URL(…)`, `new Date()`; risky config).
- Add your team's rules to `~/.codist/rules/*.yaml` or `.codist/rules/*.yaml` in the repository; they apply on
  the next review without a restart, and an `id` that matches a built-in rule replaces it.
  See `server/internal/knowledge/rules/README.md` for the format.

Built-in deterministic checks (tree-sitter) also cover: SQL concatenation, `Optional.get()`, `@Transactional`
pitfalls, mutable singleton state, `BigDecimal(double)`, empty catches, field injection, unvalidated bodies,
entities in APIs, `System.out`, constants from another domain, **remote calls per loop iteration** and
**per-iteration logging** (with summarise / DEBUG / warn-only / bounded-collection options).
The build file is checked too: Spring Boot 2 end of life, Java version, known-vulnerable versions
(log4j, commons-text, snakeyaml, jackson), dev-only dependencies on the production classpath.

### Fine-tuning (optional)

```bash
cd server && go run . export-training ~/codist-train.jsonl     # 2 examples per rule: bad -> finding, good -> clean
pip install mlx-lm
mlx_lm.lora --model Qwen/Qwen2.5-Coder-7B-Instruct --train --data <dir with train.jsonl/valid.jsonl> --iters 600
```

Fuse the adapter, convert to GGUF and `ollama create` it, then set `CODIST_MODEL`. Fine-tuning pays off once you
add many real examples from your own reviews; for a few hundred rules the knowledge base in the prompt works better.

## Memory and context

The model has no memory between calls; Codist provides it. Each prompt is assembled from the project
overview and build (Spring Boot / Java / dependencies), signatures of related types ranked by relevance (named
in the changed lines, injected, entity/repository/service), types that depend on the file, the selected
guidelines, rule results, team ignores, earlier findings in the same file, the other files of the commit, the
diff and the numbered file. When that exceeds the context window, background sections are trimmed first and the
diff and changed code always fit.

## Speed

- Rule findings appear immediately; the model runs after, file by file.
- Trivial changes (imports, comments, formatting) skip the model; answers are cached by prompt, so re-reviews of
  unchanged code take seconds; the model is kept loaded (`keep_alive` 30 min) and the answer length is capped.
- Models per job: `CODIST_MODEL` (review), `CODIST_SUMMARY_MODEL` (commit summary, e.g. `qwen2.5-coder:3b`),
  `CODIST_FIX_MODEL` and `CODIST_ASK_MODEL`. Codist passes the context between them itself; no MCP is needed.
- On 16 GB of RAM keep one 7B model, or set `OLLAMA_MAX_LOADED_MODELS=1` when using several, so Ollama swaps
  instead of running out of memory. `OLLAMA_FLASH_ATTENTION=1` and `OLLAMA_KV_CACHE_TYPE=q8_0` cut memory and
  speed up long prompts.
- Commits made while Codist was stopped are reviewed when it starts.

## Test

```bash
make test       # Go unit tests (indexer, rules, diff parser) + svelte-check
```

## How it works

- **Watching.** Every 3s the server checks the tracked branch for new commits (fetching `origin` every 30s
  in remote mode) and fingerprints the `.java` files in the working tree. Edits re-index the graph;
  new commits re-index and queue a review. The UI is updated over server-sent events.
- **Graph.** tree-sitter parses every Java file. Types become nodes, classified by annotation or naming
  (controller, service, repository, entity…). Edges are `injects` (constructor / `@Autowired` / Lombok),
  `extends`, `implements` and `uses`, resolved by imports and package.
- **Review.** The diff is mapped onto the types and members it touched, as they were at that commit.
  Rule checks then run on the changed lines:
  - `Optional.get()` with no presence check
  - SQL built by string concatenation
  - `@Transactional` on private/final methods, and self-invocation that skips it
  - mutable state in singleton beans
  - `new BigDecimal(double)`
  - empty catch blocks
  - field injection
  - `@RequestBody` without `@Valid`
  - entities returned from controllers

  The LLM gets the project overview, signatures of related types, dependents, the rule results, the diff
  and the numbered file, and returns JSON findings. A final call writes the commit summary and risk.

## GitHub webhook

Polling works with no setup. For instant reviews on push, expose the server
(`cloudflared tunnel --url http://localhost:8080`) and add a push webhook to
`https://<tunnel>/api/webhook/github` with the same secret as `CODIST_WEBHOOK_SECRET`.
