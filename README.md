# Codist

Codist reviews a Java and Spring Boot repository on your own machine. It has four parts:

- **Review**: every new commit is checked on its changed lines, then read by a local model. Each model claim is
  checked against the code before it is shown, and issues are followed until a later commit fixes them.
- **Graph**: one canvas with four levels (microservice, service, layer, method), class roles as shapes, issues as
  rings, the selected commit's changes and their impact, filters, a legend, elastic dragging and a right-click menu
  for blast radius, paths and isolating a class.
- **Incidents**: paste production logs to see each error, the line of your code it came from, and the commit
  that last changed that line. The model can suggest a cause.
- **Repository**: modules, HTTP routes, Feign calls, hotspots, the largest classes and methods that may be slow.

Nothing leaves your machine. Codist never changes your code.

## Run it

You need git. For model review, run a model server yourself, such as [Ollama](https://ollama.com):

```bash
ollama pull qwen2.5-coder:14b      # about 9 GB; use :7b on smaller machines
```

Then build Codist and run it inside a repository:

```bash
make build
cd ~/dev/your-spring-app
/path/to/codist/bin/codist         # opens http://localhost:8080
```

`codist doctor` shows what is installed and what is missing. Without a model, Codist still runs its code checks.

## Settings

Put settings in `.codist/config.lua` in the repository. All are optional:

```lua
model = { url = "http://localhost:11434", name = "qwen2.5-coder:7b", context = 8192 }
max_files = 8            -- changed files per commit the model reads; checks read all of them
history = 50             -- recent commits the model reads in the background; 0 for all. Checks cover every commit
ignore = { "target/", "build/", "generated/" }
```

The script runs with no file, network or OS access. Environment variables override it for one run:
`PORT`, `CODIST_MODEL`, `CODIST_MODEL_URL`, `CODIST_MODEL_REVIEW=false`, `CODIST_HISTORY`, `CODIST_DATA_DIR`.

## Build

`make build` builds the interface in `web/` (Svelte; needs Node) and embeds it in `bin/codist`; `make dist` makes
one file each for macOS, Linux and Windows. No C compiler is needed, and people who run Codist need neither Node nor
Go. `make dev` runs the server with a live-reloading interface. `make test` runs the tests. Data is kept in
`~/.codist/<repo>.db`, a SQLite file.

The branch picker in the top bar shows any local or remote branch read-only: its commits, graph and repository
report, read from git, while your checkout stays as it is.
