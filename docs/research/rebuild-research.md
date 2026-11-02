# Rebuild Codist smaller, measured, and model-led

Rebuild Codist as one loop: review a change, show the evidence, prepare a fix, approve or reject, learn from that. Everything else should go, which takes it from 9 screens to 3, 56 API routes to about 15, and 17,158 lines of Go to about 10,000 (targets are estimates). The most important step comes before any of that: replace the benchmark. Today's score cannot separate one model or prompt from another, so most choices below are candidates to test, not proven answers. The hardest finding is that rules found 10 of the 12 bugs found on the current benchmark and the model added 2, so "the model does most of the work" is a goal the evidence does not yet support. There is a real path toward it: narrower prompts, facts supplied by code, checks aimed at bug classes rules cannot see, and a fix step checked by the compiler. Each step needs a benchmark of about 100 bugs or more to prove itself. Codist's real advantage is deterministic Java facts given to a private local model, with fixes a person approves. This research found no product offering that exact combination, but no benchmark has tested it either.

**How to read the evidence labels.** **[Measured]** was run in this repository or on this machine. **[Code]** was read from the source and not run. **[Published]** comes from a paper or a neutral source. **[Vendor]** is a seller's own claim. **[Inferred]** is reasoning from the research. No model was run for this report.

## Rules 10, model 2 is both true and misleading

**[Measured]** The best run found 12 of 16 labels: 10 by rules and 2 by the model, both "check-then-act" races (`eval/results/20261102-085343.json`). The pipeline found 3 of 7 logic bugs: 2 by the model and 1 by a rule, on a commented-out line. Two prompt experiments did not find the remaining logic bugs, and a sceptical second call removed every model finding, including a real race (`docs/wiki/Model-quality.md`). All of this is on `7b`. **The current default, `14b`, has no full evaluation**; on a six-file test it found only a commented-out line.

The number is weak in both directions:

| Why it may overstate rules | Why it may understate the model | Why it cannot decide anything |
|---|---|---|
| Rules and benchmark were written together: at least 3 of the 10 rule hits come from rules added in the commit that added `eval/`. | **[Code]** The echo filter drops any model finding within one line of a rule finding, or with the same `ref` anywhere in the file (`service.go:577-581`). A model that also found a rule's bug gets no credit. | 16 synthetic labels in one small project with 492 lines. |
| 10 of 26 rule findings are exempted from "noise" by the `alsoTrue` pattern, and the scoring changed as outputs changed. | The model is told the rule findings first, so "added 2" measures what it adds after rules, not what it can do alone. **[Inferred]** | **[Inferred, own calculation]** 12/16 has a 95% interval of 48–93%, and 3/7 has 10–82%. A change must win 6 bugs and lose none to reach p<.05. |
| **[Inferred]** At default settings (8 files, not the eval's 100), the same commit would score about 10 of 16. | **[Code]** `ruleCovered` may drop true findings silently (see the bugs table). | Model and prompt results differ by only 1 or 2 labels, and runs at temperature 0.1 are not independent samples. |

The outside evidence sets realistic expectations. Across one 1,000-PR Python benchmark, the best prompt-based method scored F1 15.95 on `Qwen-2.5-R1-14B`, 11.63 on `Qwen-2.5-7B` and 20.85 on GPT-5 ([SWR-Bench](https://arxiv.org/html/2509.01494v2)). Eight frontier models caught 15–31% of human-flagged issues in a diff-only setup ([SWE-PRBench](https://arxiv.org/abs/2603.26130)). PR-Agent's own docs call local models "not suitable for production-level code analysis tasks" ([PR-Agent docs](https://docs.pr-agent.ai/usage-guide/changing_a_model/)). Codist has to disprove that with numbers, not assert the opposite.

### What would actually let the model do more

Rules keep producing facts and their findings are always reported. The model takes everything that requires judgement. This table lists the changes that could shift the balance, cheapest first.

| Change | Why it might work | Evidence |
|---|---|---|
| Stop hiding model findings: log every dropped finding with its reason | Shows what the model already finds | **[Code]** Several drops leave no log entry |
| Schema output, with `evidence` before `detail` | Forces a quote before an argument, and removes the retry and coercion code | **[Inferred]** No precision study found |
| One changed method per call | Frees most of the 8k window and removes distractions | Hunk-level tools beat file-level ([Sun et al.](https://arxiv.org/html/2508.18771v2)). More context lowered scores for all eight models ([SWE-PRBench](https://arxiv.org/abs/2603.26130)). Not tested here |
| Facts from code, including implicit Spring behaviour | One frontier model scored 0/19 on cross-file authorization and 0/18 on framework-applied authorization; supplying these as facts is the untested fix | **[Vendor]** [Semgrep](https://semgrep.dev/blog/2025/can-llms-detect-idors-understanding-the-boundaries-of-ai-reasoning/), on one frontier model |
| Narrow checks chosen from facts: route has no visible auth, method has shared state, `@Transactional` is self-invoked | A route graph plus model recalled 19 of 30 access-control bugs. The same model unstructured got 3, and CodeQL and Semgrep got 0 | **[Published, one preprint]** [ABSENTIA](https://arxiv.org/abs/2610.00977). Both of Codist's model wins were races |
| A small "need" loop in JSON: up to 2 rounds, using `method_body`, `callers`, `search` | Lets the model pull context without an agent loop | Model-written search beat a basic BM25 setup on code completion ([GrepRAG](https://arxiv.org/html/2601.23254v1)). Agent gains shrink on weak models ([Sifting the Noise](https://arxiv.org/abs/2601.22952)) |
| 3–5 fresh runs on risky methods, aggregated | Runs disagree a lot, so a union raises recall. Qwen-2.5-7B gained 26% F1 at n=10, with diminishing returns after 5 | **[Published]** [SWR-Bench](https://arxiv.org/html/2509.01494v2). **[Inferred]** On a laptop this means sequential runs and more time |
| Generated regression test: passes on parent, fails on the change | The parent acts as the oracle, without needing a specification | Meta: 8 of 41 reported catches were true ([JiT](https://arxiv.org/abs/2601.22832)). Yields are low, and showing the model the buggy code can make tests assert the bug ([Misguidance](https://arxiv.org/abs/2607.22883)) |
| Model prepares more of the fix, code checks it | This is real work the model can own now | Not measured at all today |

**A conflict to state plainly: should a second model pass check findings?** The evidence splits. BitsAI-CR's filter raised precision from 57% to 66% with recall down from 45.5% to 39.8%, but used a fine-tuned model, and the paper's figures are not fully consistent ([BitsAI-CR](https://arxiv.org/html/2501.15134v1)). A local same-model second look kept 90% of true positives and removed 41% of false ones, on a small sample ([Frame](https://lambdasec.github.io/Frame-Grounding-LLM-Vulnerability-Detection-with-a-Sound-Separation-Logic-Core/), vendor-authored). Against it, RovoDev's correctness judge had minimal impact ([RovoDev](https://arxiv.org/html/2601.01129v2)), Greptile called self-judgement "nearly random" ([HN excerpts](https://news.ycombinator.com/item?id=42451968), vendor), smaller models got worse when filtering in IRIS ([IRIS](https://arxiv.org/html/2405.17238v3)), and it failed in Codist itself. **Position: no veto pass.** A filter also pushes against the goal of getting more from the model. Retest it only as a rank or a label, and only once a clean-code set can show what it costs.

**Do not promise logic bugs.** The four never-found-by-`7b` bugs sit in files under 26 lines, so context is not the limit. They may need a bigger model or a generated test (**[Inferred]**).

### How to measure it

Define the goal so it can fail: **the share of accepted findings and accepted fixes that needed the model**. Today's only reading is 2 of 12 findings on a contaminated set. The bars below are my proposals.

| Measure | How | Today | Bar |
|---|---|---|---|
| Model alone, by bug class | Rules, filters and rule hints off | Never run | Report only; this shows the model's real ability |
| Model's added recall | Full pipeline versus rules only, same bugs, 3–5 seeds, paired McNemar | 2 of 16 | Wins on dev set, then confirmed on frozen holdout |
| Cost of each part | Remove RAG, facts, filters one at a time | Never run | Remove whatever does not earn its place |
| False alarms | Run on the fixed version of each bug, and on clean code; report per KLOC; hand-check a sample | 4 clean files only | No rise for a recall gain |
| Fix success | Compiles, tests pass, parent-to-fix behaviour correct | Never run | Report by fix type |
| Time and memory | Per-call timing, model digest, machine state | Same load took 212–819 s, cause unknown | Stated per review |
| Field use (local only) | Accept and reject per finding, findings per change, approve rate | None | Compare loosely with 38.7% vs 44.45% for humans ([RovoDev](https://arxiv.org/html/2601.01129v2)); about 2 comments per PR; near-100% approval is a warning sign, not success |

**Size.** Own calculation at 80% power from 60% baseline recall: about 95–130 bugs to prove a +15 point gain and about 170–250 for +10. Sources: Defects4J (835 bugs) and GitBug-Java (199 bugs), split by project, with a frozen holdout. Use bugs dated after the model's training cutoff where possible, since well-known bugs may be memorised ([LASAA](https://arxiv.org/html/2607.09979v1)). AACR-Bench's Java slice and the Keycloak part of the [Martian benchmark](https://github.com/withmartian/code-review-benchmark) can be outside checks; the Java count in each is unverified and may be small.

## What can be done better

| Area | Today | Better |
|---|---|---|
| Evaluation | One toy project; no commit, prompt hash or model digest in results; results gitignored; `054953` recorded as a `7b` run with 0 calls | Larger benchmark as above; record provenance; fail if no model ran; run at default settings too |
| Unit of review | File, one call each; tail trimmed under budget | Method, with facts |
| Silent loss | Files past 8 skip rules too; filters drop findings unlogged | Rules run on every file; tell the user what was not reviewed; log all drops |
| Reporting | Rule findings and model suspicions look alike | Label each: rule finding, model suspicion, confirmed by test |
| Fixes | Model rewrites the whole file; never compiled | Patch only; compile and run reached tests; show the result |
| Prompt | Instructions ask for dead code and "better approaches", then filters remove the results | Remove those requests; hard rules live in code, since "do not nitpick" prompts fail ([HN](https://news.ycombinator.com/item?id=42484498)) |
| Structure | `internal/review` is 7,670 lines of mixed work; rule identity is matched on strings in several places; 0% tests on API and watcher; no CI | One rule ID type; split by job; small test suite on API; CI |
| Rule storage | Four stores; only 17 of 121 YAML rules can execute | One format, marked as fact, teaching note or both |
| Rule retention | Rules stay forever | Retire or fix rules that keep being rejected; this helped most at [Google](https://arxiv.org/html/2405.13565v1) and [ByteDance](https://arxiv.org/html/2501.15134v1) |
| Privacy proof | "Offline" claim, though Maven downloads analysers on first use | Stated network policy, startup statement, network-off test buyers can run, offline install path |
| Docs | Two READMEs contradict the code | Port no prose without checking it |

**Likely bugs in the current code. All read from code, none run. Confirm each before reuse.**

| Issue | Where | Effect |
|---|---|---|
| `ruleCovered` keyword `"inject"` | `service.go:571, 608-626` | A model title such as "SQL injection" or "Command injection" matches, and is dropped silently unless the mapped rule fired. Map order decides which rule |
| Binds all interfaces, no auth | `config.go:72`, `internal/api` | Anyone who can reach port 8080 could read code and trigger commits. This conflicts with "local and private". Cross-origin posts from a browser page are also possible but untested |
| `evidence` is optional and matched as a substring | `service.go:562`, `verify.go:34-37` | Omitting the quote skips the check; a short quote passes |
| Echo filter | `service.go:577-581` | Drops a different problem if it sits within one line of a rule finding |
| Mixed code states | `service.go:477`, `impact.go:86-94` | Reviewing an old commit uses the current graph for rules |
| Tail trim | `context.go:49-52` | Changed lines near the end of a file up to 600 lines long can vanish from the numbered view |
| Unbounded caches | `service.go:59`, `context.go:141-149` | Every Java file per reviewed commit stays in memory |

## What Codist already does better

These are comparisons from reading products, not head-to-head tests.

| Strength | Compared with | Note |
|---|---|---|
| Code never leaves, no vendor account | CodeRabbit self-host needs [500+ seats](https://docs.coderabbit.ai/self-hosted/github); Qodo Enterprise is [custom, for 30+ users](https://www.qodo.ai/pricing/); Sourcegraph has a [$16K minimum](https://sourcegraph.com/pricing) | The core reason to exist |
| One binary | Kodus needs [Docker and a VM with 8 GB and 60 GB disk](https://github.com/kodustech/kodus-ai) | Still needs Ollama today |
| Works before any PR, with no Git platform | Most tools trigger on PRs, though CodeRabbit added [IDE and pre-commit CLI review](https://docs.coderabbit.ai/overview/ide-cli-review) | Catches issues earlier **[Inferred]** |
| Java call graph with an honest "confirmed" or "possible" tag, plus routes, tests and Feign links | No product found combines these offline | Accuracy has never been measured; my search was not exhaustive |
| Rule findings appear at once, about 2 s on the eval project | Agent-based tools must finish a model run first **[Inferred]** | **[Measured]** on a tiny project |
| Prepare-and-approve with stale-file refusal | Others offer click-to-apply; autonomy is rising elsewhere | Matches preference: 63% rarely or never let agents run on autopilot ([Stack Overflow pulse, self-selected](https://stackoverflow.blog/2026/05/27/agents-on-a-leash-agentic-ai-remains-mostly-monitored-at-work/)) |
| Activity log shows everything the model saw and said | The product research did not turn up an equivalent, but did not check for one | Fits the 79% who want source attribution ([The Register on SO 2026](https://www.theregister.com/software/2026/10/10/stack-overflow-survey-finds-devs-hooked-on-ai-but-not-totally-sold-on-its-judgment/5301662)) |
| Plain, overridable knowledge: 115 bug patterns, 121 rules, 18 prompts | Custom rules are common; a shipped, editable corpus looks less so **[Inferred]** | Provenance unverified |
| Records its failures honestly in the wiki | Almost every public benchmark is vendor-run with the vendor first ([Qodo's survey](https://www.qodo.ai/glossary/what-is-ai-code-review-benchmarks/)) | A real trust asset |

**Where it loses, and should not compete:** raw detection on hard bugs, PR-bot workflow, many languages, enterprise procurement. The closest open-source relative is [Alibaba's Open Code Review](https://github.com/alibaba/open-code-review): Go, deterministic plus LLM, Ollama support, 49,285 stars. It is the one to measure against and watch. Its rules' Java depth was not read, so its limits are not established. Take single ideas from others; do not copy any one product.

## Make the model do most of the work, and keep RAG small

| Stays code (facts and checks) | Moves to the model (judgement) | Cut |
|---|---|---|
| Parsing, diff, line numbers, call graph, routes, tests | Which related code it needs, through "need" | Commit summary call; compute risk from findings |
| Exact rules, always reported | Whether a finding matters here, and severity | Regex commit message checks and title-based grouping |
| Quote check, line snap | Which of the top 3 guidelines applies | Separate summary, fix and ask models |
| Compile and test gate, patch apply on approval only | Writing the explanation and the patch | Five retry prompts; replace them with one compile-and-feedback loop |

**RAG is three small sources, not a vector store.** Evidence on retrievers really does conflict, and none of it covers Java review on small models at 8k:

- **For BM25:** on small code models in C++ completion, BM25 took exact match from 24.79 to 53.76, while call-relationship retrieval reached only 26.99 ([Tencent](https://arxiv.org/html/2505.15179v1)).
- **For embeddings:** Recall@20 of 0.707 against 0.475 lexical, though "no retrieval family dominates" ([Agent Retrieval Bench](https://huggingface.co/papers/2607.24882)).
- **For structure:** RepoMap had the best score at an 8k budget in the same bench.
- **Against more context:** see SWE-PRBench above, and RovoDev found extra context gave minimal gain.
- **For more context:** 48% of sampled false positives came from missing context ([SWR-Bench](https://arxiv.org/html/2509.01494v2)).

The call graph is therefore not proven as a retriever. Its value is in answering specific questions ("is this reached from a route?"), and it should be evaluated per bug class.

| Source | Implementation | Size |
|---|---|---|
| Guidelines | Keep BM25. Query from the changed method and annotations, not the whole file; filter by file kind; top 3 at about 6 lines | About 150 lines, no dependencies |
| Team standards | Split `AGENTS.md`, `CLAUDE.md` and similar by heading and retrieve matching sections, instead of 3,000 chars on every file. Repo overviews do not help; specifics do ([ETH Zurich](https://arxiv.org/abs/2602.11988)) | About 20 lines |
| Code facts | Method body, signatures of callers and callees, routes, tests, from the existing index, with a tag for how each was resolved | About 100 lines |
| History | Last 3 commits touching the method, via `git log -L` | About 50 lines |
| Model lookups (second step) | "Need" loop over method chunks; merge adjacent chunks | About 100 lines |

**Starting budget for 8k, to tune by test:** system 600, answer reserve 800–1,500, changed method and diff 2,000, class fields and annotations 300, facts 800, guidelines 450, standards 300, memory and rejected notes 200, history 150. That totals about 5,600–6,300 tokens, leaving room for the "need" loop.

**Cheapest test in the whole plan:** add expected guideline IDs per bug to `labels.json` and report Recall@3 and Recall@6 with no model. It runs in seconds. **Add embeddings only if this shows misses BM25 cannot fix**, then use a small embedder, a flat vector file and rank fusion. No SQLite extension or vector database is needed for a few thousand vectors. No reranker.

Fine-tuning comes last. BM25 alone beat it in the one direct comparison on small code models (53.76 vs 44.20, [Tencent](https://arxiv.org/html/2505.15179v1)), production filters used 5,000–18,000 samples ([BitsAI-CR](https://arxiv.org/html/2501.15134v1)), and the 240 local examples only teach what rules already catch.

## Better UI: one screen, three panes

The work is a diff, yet today the diff is a 63-line tab beside a 775-line graph. Findings appear in four places and code in three. Target: `web/src` from 7,497 to about 2,300 lines and `web/dist` from 1.6 MB to about 250 KB (estimates). Screens become **Review, Knowledge, Activity**. AI comments that are mostly code get acted on more: 23.2% vs 4.2% with no code ([Sun et al.](https://arxiv.org/html/2508.18771v2)).

| Component | What it does | Taken from |
|---|---|---|
| Changes list (left) | Uncommitted and recent commits; model status in footer | `CommitsTab`, `Sidebar` |
| One scrolling diff (centre) | All changed files; findings inline under their lines | [Zed](https://git.secluded.site/zed/blob/vue3/docs/src/git.md) shape; merges `DiffTab`, `CodeTab` |
| Finding row | Short text, label by source, approve or reject | `FindingCard` |
| Evidence panel (right) | Quoted line, callers, routes, tests, guideline used, link to exact prompt | `ImpactCard` |
| Patch sheet | Patch opens on request, showing compile and test result; reject with a reason | `FixDialog`, already has remembered reasons |
| Keyboard map | `j/k` finding, `n` next unreviewed file, `f` prepare fix, `r` reject, `Cmd+Enter` apply shown patch, `e` evidence, `?` help | [Gerrit](https://gerrit-review.googlesource.com/Documentation/user-review-ui.html), [Reviewable](https://docs.reviewable.io/reviews.html) |
| Reviewed marks | Mark a file reviewed; show only what changed since last look | Same two tools |
| Step progress | "File 3 of 8, 12 s", rules first. Token streaming is not needed | `/api/events` |

Rate the finding and the fix separately, because a real bug with a wrong fix is common ([HN](https://news.ycombinator.com/item?id=46766961)) and must not teach Codist the finding was bad. Aim approval at the author only: Meta had to roll back showing patches to reviewers, after they raised time spent by 6.7% ([MetaMateCR](https://arxiv.org/html/2507.13499v1)). Stay with the small web UI plus a real CLI. A terminal handles patch approval badly, and an IntelliJ plugin adds a separate project and release cycle. No study measures which surface developers prefer, so this is judgement. One study found manually triggered comments addressed far more often than automatic ones (22.2% vs 0.5% for one tool, [Sun et al.](https://arxiv.org/html/2508.18771v2)). That concerns PR comments, so I do not read it as a reason to turn model review off. It is a reason to keep results in a quiet queue and to measure acceptance.

## Optimisation: nearly all the time is model time

**[Measured]** The app takes about 0.5 s to first response and 20 MB idle on the eval project; a synthetic 3,000-file repo indexed in 789 ms at about 190 MB. A 9 GB model makes binary size almost irrelevant to users, so do the first rows first.

| Change | Gain | Status |
|---|---|---|
| Record per-call timing | Explains why the same load took 212–819 s | Needed before any speed claim |
| Remove commit summary call | About 6% of eval calls | **[Measured]** count |
| Remove fix prefetch, fix on click | Up to 3 calls per review | **[Code]** |
| Per-method prompts | Shorter prompts, but possibly more calls | Test |
| Stable prompt prefix, method last | Reuses cached prefix | **[Inferred]** Prefill share not measured |
| Thinking off by default; budgeted on risky methods only | `qwen3:8b` took 1–5 min per file with thinking but found a bug others missed | **[Measured]** on six files |
| Shorter `keep_alive` than 30 minutes | Frees 10 GB sooner | **[Inferred]** One-line test |
| Do not wait for PMD or SpotBugs before the model | Removes first-file delay | **[Code]** |
| Run one call at a time on laptops | No aggregate gain from concurrency in one Mac test | **[Published]** [Shin](https://jaesolshin.com/posts/apple-silicon-llm-backends/) |
| Keep 8k context for now | `7b` found the same bugs at 8k and 16k, but the eval's largest file is 70 lines, so this is not proven on real files | **[Measured]**, narrow |
| Reindex changed files only; bound source cache | Saves work on each save; stops memory growth | About 50 lines |
| `-trimpath -s -w` | 17.9 MB to 12.7 MB | **[Measured]** |
| System fonts | -312 KB | **[Measured]** size |
| Remove the graph libraries | Estimated 60–70% of JS | **[Inferred]** Not rebuilt |

**Stay in Go.** A rewrite repeats about 20,000 lines and still links a C parser, to save a few MB. After the cuts, expect 8–9 MB stripped (estimate).

## Better model selection: test, do not assume

The candidates below are not proven. **No published benchmark measures any of them on Java review at 8k.** You chose `14b` deliberately, so the question is for the eval to answer, not for me to overrule. Still, it shows 10 GB loaded on a 16 GB machine **[Measured]**, above one guide's 60–70% rule of thumb ([InsiderLLM](https://insiderllm.com/guides/best-local-llms-mac-2026/)) before a JVM and an IDE are counted.

| Memory | Test these | Why | Evidence quality |
|---|---|---|---|
| 8 GB | `qwen2.5-coder:3b`, `qwen3.5:4b`, `granite4.2:3b` | Fit in memory | `3b` scored 11 of 15 with more noise; the others untested |
| 16 GB | `qwen2.5-coder:14b` (current), `qwen2.5-coder:7b` (baseline), **`qwen3.5:9b`**, `gemma4:12b` | `9b` scored 82% at 24 s per task at `num_ctx` 8192 with thinking off | One independent bench, 10 of 11 tasks Python, agent work not review ([kuroko](https://dev.to/kuroko1t/benchmarking-local-coding-llms-11-realistic-tasks-232-runs-and-the-bugs-my-bench-found-in-my-46pl)) |
| 32 GB | **`qwen3.8:27b`**, `qwen3.6:27b` | `3.8` is the only small-enough model on the current open-weight index list | Index 20 vs 58 for the frontier leader ([Artificial Analysis](https://artificialanalysis.ai/models/open-source)). Coding scores are **[Vendor]**, at 256K context with thinking on |

Avoid `codestral`: its [licence](https://ollama.com/library/codestral/tags) bars production use. Good detection does not imply good filtering: in IRIS, all open models found more than CodeQL alone but had higher false discovery ([IRIS](https://arxiv.org/html/2405.17238v3)).

**Serving.** Code to an OpenAI-compatible interface with JSON schema, plus small adapters for thinking and context. Keep Ollama now. Separately spike a `llama-server` child process inside the binary, on Apple Silicon first, which would remove the Ollama install; the weights (2–18 GB) can never be inside the binary. The cost is owning runtime updates and macOS signing, which was not tested. Do not build directly on MLX. Upgrading models may also change runner: since [Ollama v0.40.0](https://github.com/ollama/ollama/releases/tag/v0.40.0), named newer architectures run on MLX on Apple Silicon by default, while `14b` still shows `llamacpp` **[Measured]**, so timing must be re-measured per model.

## Features worth adding, and what may be added later

All of these strengthen the loop. Effort is S under a day, M a few days. Effort is an estimate.

| Add | Effort | Why |
|---|---|---|
| Larger eval and retrieval test | M | Decides everything else |
| Bind `127.0.0.1` by default, with a session token and Origin check | S | Closes the likely hole |
| No-egress proof and statement | S | "Will InfoSec let this in?" is the buyer's question; 53.3% report rules blocking agents ([SO 2025](https://survey.stackoverflow.co/2025/ai)). Hidden calls break offline use ([OpenCode #16117](https://github.com/anomalyco/opencode/issues/16117)) |
| Compile the fix and run reached tests before showing it | M | Already on the roadmap. No listed tool was found marketing this locally, though internal checks may be undisclosed. Fits how 76.5% verify AI output, by running it ([SO 2026](https://survey.stackoverflow.co/2026/ai/data)) |
| `codist check --staged` hook; `codist scan --since 7d`; `codist review <range> --json` | S each | Rule results at once, model review follows as usual, never blocks a commit |
| Reject with reason on any finding; per-rule accept rate; demote guidelines the team keeps rejecting | S–M | Filtering by real reactions worked where prompting failed ([RovoDev](https://arxiv.org/html/2601.01129v2), [Greptile, vendor](https://news.ycombinator.com/item?id=42451968)). Check against recall so rejections do not hide real bugs |
| Spring facts per method: transaction opened by caller, proxy skipped by self-invocation, auth on route | M | Linters miss these. The priority order is my hypothesis; no survey ranks Spring pains. Check against existing rules such as `tx-self-invocation` |
| Route-to-auth check | M | ABSENTIA's shape, with the route data already present |
| Feign and DTO contract drift, as a rule | M | **[Inferred]** Index already holds the data; no study found |

**Maybe, only if the eval or users ask:**

| Maybe | Condition |
|---|---|
| Embeddings | Retrieval test shows misses |
| OpenRewrite `rewrite:dryRun` as a fix source, with the user's Maven fetching recipes | Mechanical fixes prove common. Do not bundle recipes: most are [MSAL-licensed](https://docs.openrewrite.org/licensing/openrewrite-licensing). No study of LLM recipe selection found |
| scip-java for compiler-resolved facts | Tree-sitter accuracy proves too low; needs a full compile |
| Opengrep with Codist-written rules | Needs no JVM, but is a second binary beside the first. Do not ship [Semgrep's rules](https://docs.semgrep.dev/faq/comparisons/opengrep) without legal review |
| Error Prone | A compile step already exists; needs JDK 21 |
| Team memory through committed `.codist/` files | Teams ask |
| Local classifier on accept and reject data | Thousands of clean decisions exist |
| IntelliJ, through `--json` | Users ask |
| Pointing the base URL at a shared GPU box | Teams ask. Needs TLS and access control |

**Do not integrate, on licence grounds. My reading of the research, not legal advice:** CodeQL's [terms](https://github.com/github/codeql-cli-binaries/blob/main/LICENSE.md) bar private code without a paid licence and bar redistribution. Sonar's [analyser licence](https://www.sonarsource.com/license/ssal/) excludes using outside AI on its data. Qodana's Spring inspections are [paid](https://www.jetbrains.com/help/qodana/2026.2/pricing.html). Static tools are evidence, not a safety net: seven tools detected only 12.7% of real Java vulnerabilities ([FSE 2023](https://sites.google.com/view/java-sast-study/home)).

## What is too much

No usage data exists, so these rest on purpose and cost. Features you already removed stay out; this list only covers what remains, plus leftovers such as the `IssuesHub` name.

**These four need your yes. You did not ask to remove them.**

| Cut? | Size today | Case for cutting | Case against |
|---|---|---|---|
| **Visual code graph** | About 1,600 lines, 21% of UI, and most of the JS | Largest UI cost; the graph data stays as facts and a text impact list | It is the most visible and distinctive screen |
| **Incidents** | 1,376 Go + about 450 UI + 3 prompts | A second product; accuracy never measured | Log parser is real work; could become its own tool |
| **Release** | 412 Go + 183 UI | Not on a change's path | Teams may like the report; a CLI scan could replace part |
| **Team** (with My work, profiles) | 562 Go + 260 UI | Not review; rates people, close to the AI developer summaries you removed | Holds no AI text today; `team.go` makes no model calls |

**Also recommended, lower stakes:**

| Cut | Reason |
|---|---|
| Repository report; Branches page and lanes | Reporting, not action. Keep "look at this branch" |
| Feature flags | Three screens need no switches |
| Remote mode, GitHub token, webhook | Against local-first; check nobody uses it |
| jdtls | Heavy; compile check replaces most of its value |
| Commit plan, free-form edit, commit reword | An editor's or Git's job |
| `export-training` | Returns if fine-tuning does |
| Issue tracker as it stands (`tracker.go` and friends, 914 lines) | Merge into a dismissal store of about 200 lines, keeping the line fingerprint |

**What to carry over:** Java rules and their tests, call graph and impact, 115 bug patterns and 121 rules, prompts as files, fix guardrails (stale-file refusal), issue identity by rule and code line, and the recorded failed experiments. Log parsing goes only if you keep Incidents.

| Size | Now | Target (estimate) |
|---|---:|---:|
| Screens | 9 | 3 |
| API routes | 56 | 12–15 |
| Go, no tests | 17,158 lines | About 10,000 |
| `web/src` | 7,497 lines | About 2,300 |
| `web/dist` | 1.6 MB | About 250 KB |
| Stripped binary | 12.7 MB | 8–9 MB |

## Open decisions and first steps

| Decision for you | My recommendation |
|---|---|
| Cut the visual code graph? | Yes, text impact stays |
| Cut Incidents? | Yes; set aside the log parser |
| Cut Release? | Yes |
| Cut Team and My work? | Yes |
| What counts as "the model does most of the work"? | Share of accepted findings and fixes that needed the model, as above. Accept that rules may still produce more raw detections |
| Keep `14b` as default until the eval runs? | Yes for now; let the eval decide on 16 GB |
| Is 16 GB the design target, or do you accept 32 GB for full quality? | Target 16 GB and report a 32 GB result. This may decide how far the model can go |
| Keep the Ollama install, or spike the embedded runtime? | Keep it now; spike after the eval |
| Are PMD and SpotBugs in the core, given the Maven download? | Optional, off the model's path, with a documented offline install. They have never been measured in `eval/` |
| Who is it for: one developer, or small regulated teams? | Build for one developer first. Demand from regulated teams is **unproven**: no sizing or named buyers were found, and vendor framing is most of the evidence |

**Order of work:**

| Step | Do | Finished when |
|---|---|---|
| 1 | Fix the harness: provenance, per-call timing, fail when no model ran, tighter scoring, rule, filter, RAG and facts switches, logged drops | Results are reproducible and explainable |
| 2 | Confirm the likely bugs with small tests; decide the cuts above | The `inject` and bind questions have answers |
| 3 | Run today's 16 labels on the four 16 GB candidates, rules on and off, plus the retrieval test. Treat as smoke test only | `14b` finally has a result, with intervals |
| 4 | Build the larger set: about 100 bugs, fixed-version false alarm set, frozen holdout by project | Dev and holdout exist before any tuning |
| 5 | Start the new core in Go: port rules, index, knowledge and prompts; loopback and token from day one; network policy | Loop works from the CLI at rules-plus-model parity |
| 6 | Add one experiment at a time, in the order of the "let the model do more" table; keep only what wins | Each has a paired result |
| 7 | Add the compile-and-test gate, and then the one-screen UI | Fix success has its first number |

## Conclusion

The rebuild is less a code project than a change in how decisions get made. Codist's four days of fast work produced real assets and one trap: a benchmark built alongside the rules it scores, with attribution that can hide the model's contribution. Until step 4 exists, neither "rules are enough" nor "the model is good" can be defended, and that includes this report's recommendations.

The goal may also need reframing. A small local model may never out-detect rules by count, and that would not be a failure. The more reachable version is that code supplies the facts, while the model decides what matters, explains it, targets what rules cannot see, and prepares a checked fix. If the larger benchmark shows the 16 GB tier cannot do this, the honest choices are a higher hardware target or a more modest promise, and the eval will show which.
