# Knowledge, prompts and memory

## Memory screen

**Memory** lists what this repository has added on top of the base knowledge: what people taught it, rejected fixes,
lessons from history, ignores and custom rules. Add or remove items, and share one with the team through
`.codist/memory.yaml`. Items are added to the prompts of later reviews and fixes; the model itself is not retrained.

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
