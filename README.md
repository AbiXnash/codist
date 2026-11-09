# Codist

Codist reviews a Java and Spring Boot repository on your own machine. It runs as one program next to your code,
watches the repository, and shows what it finds in your browser:

- **Review**: every new commit is checked on its changed lines, then read by a local AI model. Each model claim is
  checked against the code before it is shown, and issues are followed until a later commit fixes them. A claim
  Codist cannot back, or that the code contradicts, is shown as a quiet note with the reason; it opens no issue.
- **Fix**: for a real bug, the model prepares a small fix and says why. You see the current and proposed code side by
  side, the fix is built and tested in a separate copy first, and only when you approve is it committed (just its own
  files). Tell it how you want it fixed, or reject it and say why; it remembers.
- **Activity**: what Codist is doing right now: what code checks, the AI model and behaviour runs are working on, what
  waits and what just finished, every model call with its prompt and reply, and the log as it happens.
- **Graph**: your services, classes and methods on one canvas, with Feign calls between services, what the selected
  commit changed and what that change can affect.
- **Incidents**: paste production logs to see each error, the line of your code it came from, and the commit that
  last changed that line. The model can suggest a cause.
- **Repository**: modules, HTTP routes, Feign calls, hotspots, the largest classes and methods that may be slow.
- **Behaviour** (optional): runs the methods each commit changed on the code before and after it, with the same
  inputs, and shows what now behaves differently, so you can say whether it was meant.

Nothing leaves your machine. Codist never changes your code unless you approve a fix.

## How well it finds bugs

Codist is measured on three test projects with planted bugs. Each is built commit by commit by a script in
[codist-eval](https://github.com/abxengg/codist-eval/tree/main/testbed), with an answer key kept outside the project.
A bug counts as found when an alarm (not a note) lands within 6 lines of it; a clean commit fails if it gets any alarm.
The model is `qwen2.5-coder:14b` on a 16 GB laptop.

| Project | What it is | Bugs found | Clean commits with an alarm |
|---|---|---|---|
| [records-lab](https://github.com/AbiXnash/records-lab) | a Spring Boot gateway: JSON, XML, CSV and GraphQL over JDBC | 18 of 19 | 0 of 5 |
| [shop-lab](https://github.com/AbiXnash/shop-lab) | two Spring Boot services: Feign, Redis, Spring Batch, JDBC | 20 of 24 | 0 of 6 |
| [maze-lab](https://github.com/AbiXnash/maze-lab) | a Java Swing game written to a design spec | 15 of 19 | 2 of 3 (both low) |

Code checks alone, with no model, find 11 of 19, 8 of 24 and 3 of 19: every known Java mistake, and code that breaks
a habit the project follows elsewhere. The model finds the bugs that need reading the change against your rules, and
behaviour runs confirm many logic bugs on the game. Each run's full result is in
[codist-eval/testbed](https://github.com/abxengg/codist-eval/tree/main/testbed).

## Install

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/abxengg/codist/remaster/install.sh | sh
```

**Windows** (PowerShell)

```powershell
irm https://raw.githubusercontent.com/abxengg/codist/remaster/install.ps1 | iex
```

**Docker** (image includes git, a JDK and Maven, for amd64 and arm64)

```bash
cd ~/dev/your-spring-app
docker run --rm -p 8080:8080 -v "$PWD:/repo" -v codist-data:/data \
  --add-host=host.docker.internal:host-gateway ghcr.io/abxengg/codist
```

It reviews the folder mounted at `/repo`, keeps its database in the `codist-data` volume, and reaches Ollama on your
machine at `host.docker.internal:11434` (set `-e CODIST_MODEL_URL=...` for another address). Open
http://localhost:8080. Fixes are committed to the mounted repository only after you approve them, as usual; to
commit as yourself, add `-e GIT_AUTHOR_NAME="$(git config user.name)" -e GIT_AUTHOR_EMAIL="$(git config user.email)"
-e GIT_COMMITTER_NAME="$(git config user.name)" -e GIT_COMMITTER_EMAIL="$(git config user.email)"`.

**With Go** 1.26 or later: `go install github.com/abxengg/codist@latest`

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
`CODIST_DATA_DIR`, `CODIST_LOG=debug`, `CODIST_LOG_JSON=1`.

## Known issues

What does not work yet, and what is planned, is tracked in [the open work list](https://github.com/abxengg/codist/issues/6). The most important:

- **Merge commits are not reviewed.** A hand-resolved conflict is read as a commit that changes nothing ([codist-git#1](https://github.com/abxengg/codist-git/issues/1)).
- **Very large commits and histories are reviewed thinly.** The AI reads the first 8 changed files of a commit by name, and checking a history of a million commits takes about a day ([codist-review#1](https://github.com/abxengg/codist-review/issues/1), [#1](https://github.com/abxengg/codist/issues/1)).
- **A written spec is followed only partly.** With your rules in `.codist/rules.md`, the model catches most broken
  rules, but on the game test lab it missed changed numbers the rules state (20 s became 10 s, 60 fps became 30) and
  once trusted its own memory over the rules. Behaviour runs catch some of these.
- **Windows and Linux have not been tried on a real machine yet** ([#4](https://github.com/abxengg/codist/issues/4)).

## How it is built

Codist is split into modules, each its own repository, and built into this one program:

| Repository | What it holds |
|---|---|
| [codist](https://github.com/abxengg/codist) | this app: settings, the HTTP API and the review lanes |
| [codist-review](https://github.com/abxengg/codist-review) | how a commit is reviewed, the issue store, and what the Activity page shows |
| [codist-fix](https://github.com/abxengg/codist-fix) | preparing, checking and applying fixes a person approves |
| [codist-behaviour](https://github.com/abxengg/codist-behaviour) | running changed methods before and after a commit |
| [codist-java](https://github.com/abxengg/codist-java) | the Java parser, code checks, class graph and call graph |
| [codist-git](https://github.com/abxengg/codist-git) | reading a repository through git, and a test repository builder |
| [codist-model](https://github.com/abxengg/codist-model) | the model server client, and reading production logs into incidents |
| [codist-ui](https://github.com/abxengg/codist-ui) | the Svelte interface, embedded already built |
| [codist-eval](https://github.com/abxengg/codist-eval) | the test projects with planted bugs and their scores, the benchmark, and the behaviour-difference experiment |

[docs/architecture.md](docs/architecture.md) shows how they fit together, and
[docs/development.md](docs/development.md) how to work on them: one script clones them all side by side.
[docs/ai-engineering.md](docs/ai-engineering.md) maps the ideas in Chip Huyen's *AI Engineering* to Codist.

```bash
make build    # bin/codist; no C compiler needed
make test
make dist     # one file each for macOS, Linux and Windows
```

Data is kept in `~/.codist/<repo>-<hash>.db`, a SQLite file named after the repository's folder and full path.

## Licence

Source available under the [PolyForm Noncommercial License 1.0.0](LICENSE): free for any noncommercial purpose, such
as personal study, research, hobby projects and testing, and for use by charities, schools, public research and
public health bodies and government institutions. **Using it for a business, or in anything sold, needs a commercial
licence** from the author: open an issue at [github.com/abxengg/codist](https://github.com/abxengg/codist/issues)
to ask. Third-party code included in Codist keeps its own licence.

Required Notice: Copyright 2026 Abinash (https://github.com/AbiXnash)
