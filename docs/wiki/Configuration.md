# Configuration

Environment variables and the GitHub webhook.

## Configuration

All settings are environment variables:

| Env var | Default | Meaning |
|---|---|---|
| `CODIST_REPO` | – | Local git checkout to watch |
| `CODIST_REPO_URL` | – | Git URL to clone into `~/.codist/repos` and follow (`origin/<branch>`) |
| `CODIST_BRANCH` | current branch | Branch to follow |
| `CODIST_FETCH_REMOTE` | `false` | With `CODIST_REPO`: follow `origin/<branch>` instead of the local branch |
| `CODIST_MODEL` | `qwen2.5-coder:14b` | Ollama model for review |
| `CODIST_SUMMARY_MODEL` | same as `CODIST_MODEL` | Optional model for commit summaries |
| `CODIST_FIX_MODEL` | same as `CODIST_MODEL` | Optional model for fixes and code changes |
| `CODIST_ASK_MODEL` | same as `CODIST_MODEL` | Optional model for Ask and incident explanations |
| `OLLAMA_URL` | `http://localhost:11434` | Ollama-compatible endpoint |
| `CODIST_NUM_CTX` | `8192` | Context window requested from the model |
| `CODIST_MODEL_REVIEW` | `true` | `false` makes reviews use rules and analysers only, in seconds. Fixes and Ask still use the model. |
| `CODIST_FEATURES` | | An extra [features.json](Feature-flags.md), applied last |
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

## GitHub webhook

Polling works with no setup. For instant reviews on push, expose the server
(`cloudflared tunnel --url http://localhost:8080`) and add a push webhook to
`https://<tunnel>/api/webhook/github` with the same secret as `CODIST_WEBHOOK_SECRET`.
