# Architecture

How Codist works inside.

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
