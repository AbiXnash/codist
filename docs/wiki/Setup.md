# Setup

Everything needed to install and run Codist.

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
**`qwen2.5-coder:14b`**, which needs about 10 GB free while it runs, so 16 GB of memory is its practical minimum. On smaller machines, `codist setup` picks a smaller model.

```bash
ollama pull qwen2.5-coder:14b
```

Use any Ollama model that follows instructions and returns JSON reasonably well — Codist asks for
strict JSON and retries once on malformed output, but small or chat-only models produce weaker findings.
The model gets an 8k-token context by default (`CODIST_NUM_CTX`).

Different jobs can use different models, which is useful on machines that can only keep one model in memory:

| Env var | Used for | Suggested |
|---|---|---|
| `CODIST_MODEL` | file review and analysis | `qwen2.5-coder:14b` (default) |
| `CODIST_SUMMARY_MODEL` | commit summaries | a smaller model, e.g. `qwen2.5-coder:3b` |
| `CODIST_FIX_MODEL` | suggested fixes and code changes | a stronger model if you have RAM, e.g. `qwen2.5-coder:14b` |
| `CODIST_ASK_MODEL` | Ask tab and incident root-cause explanations | same as `CODIST_FIX_MODEL` |

Codist keeps the model loaded for 30 minutes (`keep_alive`), caches answers, and runs files one at a
time. See [Speed](#speed) for memory tuning.

## Alternatives

**Another Qwen2.5-Coder size.** `qwen2.5-coder:7b` or `:3b` on a low-memory laptop, or `:32b` when you
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
ollama pull qwen2.5-coder:14b        # in another terminal if Ollama is not running
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
- **"Model … is not pulled"** in the UI — run `ollama pull qwen2.5-coder:14b`, or set `CODIST_MODEL` to a
  model you already have (`ollama list`).
- **Ollama is not reachable** — start it with `ollama serve`, or set `OLLAMA_URL` if it runs elsewhere.
- **Out of memory with several models** — set `OLLAMA_MAX_LOADED_MODELS=1` so Ollama swaps models
  instead of loading them all.
