# Codist

Codist reviews a Java and Spring Boot repository on your own machine. It runs as one program next to your code,
watches the repository, and shows what it finds in your browser:

- **Review**: every new commit is checked on its changed lines, then read by a local AI model. Each model claim is
  checked against the code before it is shown, and issues are followed until a later commit fixes them.
- **Graph**: your services, classes and methods on one canvas, with Feign calls between services, what the selected
  commit changed and what that change can affect.
- **Incidents**: paste production logs to see each error, the line of your code it came from, and the commit that
  last changed that line. The model can suggest a cause.
- **Repository**: modules, HTTP routes, Feign calls, hotspots, the largest classes and methods that may be slow.
- **Behaviour** (optional): runs the methods each commit changed on the code before and after it, with the same
  inputs, and shows what now behaves differently, so you can say whether it was meant.

Nothing leaves your machine. Codist never changes your code.

## Install

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/AbiXnash/codist/remaster/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/AbiXnash/codist/remaster/install.ps1 | iex
```

The script downloads the program for your system (or builds it from source if Go is installed and there is no
release yet), puts it on your path, and checks git, Java and the model server. Then:

```bash
cd ~/dev/your-spring-app
codist            # opens http://localhost:8080
```

You need git. Everything else is optional: without a model server Codist still runs its code checks. To add the AI
review, install [Ollama](https://ollama.com) and pull a model:

```bash
ollama pull qwen2.5-coder:7b       # about 5 GB; use :14b with 16 GB of memory or more
```

The full walk-through, with settings and fixes for common problems, is in
[docs/getting-started.md](docs/getting-started.md).

## Settings

Put settings in `.codist/config.lua` in the repository. All are optional:

```lua
model = { url = "http://localhost:11434", name = "qwen2.5-coder:7b", context = 8192 }
max_files = 8            -- changed files per commit the model reads; checks read all of them
history = 50             -- recent commits the model reads in the background; 0 for all. Checks cover every commit
ignore = { "target/", "build/", "generated/" }
behaviour = false        -- run changed methods before and after each recent commit; runs your code, needs a JDK
```

The script runs with no file, network or OS access. Environment variables override it for one run:
`PORT`, `CODIST_MODEL`, `CODIST_MODEL_URL`, `CODIST_MODEL_REVIEW=false`, `CODIST_HISTORY`, `CODIST_BEHAVIOUR=1`,
`CODIST_DATA_DIR`.

## How it is built

Codist is split into modules, each its own repository, and built into this one program:

| Repository | What it holds |
|---|---|
| [codist](https://github.com/AbiXnash/codist) | this app: settings, the HTTP API and the review lanes |
| [codist-review](https://github.com/AbiXnash/codist-review) | how a commit is reviewed, and the issue store |
| [codist-behaviour](https://github.com/AbiXnash/codist-behaviour) | running changed methods before and after a commit |
| [codist-java](https://github.com/AbiXnash/codist-java) | the Java parser, code checks, class graph and call graph |
| [codist-git](https://github.com/AbiXnash/codist-git) | reading a repository through git, and a test repository builder |
| [codist-model](https://github.com/AbiXnash/codist-model) | the model server client, and reading production logs into incidents |
| [codist-ui](https://github.com/AbiXnash/codist-ui) | the Svelte interface, embedded already built |
| [codist-eval](https://github.com/AbiXnash/codist-eval) | the benchmark and the behaviour-difference experiment |

[docs/architecture.md](docs/architecture.md) shows how they fit together, and
[docs/development.md](docs/development.md) how to work on them: one script clones them all side by side.

```bash
make build    # bin/codist; no C compiler needed
make test
make dist     # one file each for macOS, Linux and Windows
```

Data is kept in `~/.codist/<repo>.db`, a SQLite file.
