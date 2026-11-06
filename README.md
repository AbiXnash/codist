# Codist

Codist reviews a Java and Spring Boot repository on your own machine. It has four parts:

- **Review**: every new commit is checked on its changed lines, then read by a local model. Each model claim is
  checked against the code before it is shown, and issues are followed until a later commit fixes them.
- **Graph**: services and the calls between them, then the classes in a service by layer, then a class's methods
  with what calls them and what they call.
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
ignore = { "target/", "build/", "generated/" }
```

The script runs with no file, network or OS access. Environment variables override it for one run:
`PORT`, `CODIST_MODEL`, `CODIST_MODEL_URL`, `CODIST_MODEL_REVIEW=false`, `CODIST_DATA_DIR`.

## Build

`make build` makes `bin/codist`; `make dist` makes one file each for macOS, Linux and Windows. No C compiler is
needed. `make test` runs the tests. Data is kept in `~/.codist/<repo>.db`, a SQLite file.
