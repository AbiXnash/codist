# Getting started

This takes about ten minutes, most of it downloading a model.

## 1. Install Codist

**macOS and Linux**

```bash
curl -fsSL https://raw.githubusercontent.com/abxengg/codist/remaster/install.sh | sh
```

**Windows**, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/abxengg/codist/remaster/install.ps1 | iex
```

The script puts one file, `codist` (`codist.exe` on Windows), in `~/.local/bin` (`%LOCALAPPDATA%\codist\bin` on
Windows), and then checks what Codist needs:

```
Checking what Codist needs:
  ok       git 2.54.0
  ok       java (optional; for checks that run code)
  -        no model server at http://localhost:11434 (optional; Codist still runs its code checks)
```

If it says to add a folder to your `PATH`, do that and open a new terminal.

You can choose a version or a folder:

```bash
CODIST_VERSION=v0.1.0 CODIST_BIN_DIR=/usr/local/bin sh install.sh
CODIST_FROM_SOURCE=1 sh install.sh      # build with Go instead of downloading
```

### What Codist needs

| | Needed? | Why |
|---|---|---|
| git | yes | Codist reads your commits through it |
| a model server | no | for the AI review and incident causes; code checks work without it |
| Java 21 | no | only for checks that run code |
| Node, Go, a C compiler | no | only to work on Codist itself |

## 2. Add a model (optional, recommended)

Codist does not contain an AI model. You run a model server and Codist connects to it. The simplest is
[Ollama](https://ollama.com):

```bash
ollama pull qwen2.5-coder:7b     # about 5 GB, fine on 8 GB of memory
ollama pull qwen2.5-coder:14b    # about 9 GB, better reviews, wants 16 GB
```

Ollama listens on `http://localhost:11434`, which is where Codist looks. For another address or model, see
[Settings](#4-settings).

On a laptop the model takes minutes per commit, so Codist reads the newest commits first and, by default, only the
last 50. The quick code checks still cover every commit.

## 3. Run it on a repository

```bash
cd ~/dev/your-spring-app
codist
```

Your browser opens `http://localhost:8080`. The first time, Codist:

1. reads all the Java code (about a second for a thousand files);
2. checks the whole code once, so problems already there show up, each credited to the commit that last changed
   its line;
3. runs the code checks on every commit, which takes seconds;
4. lets the model read recent commits in the background, newest first. New commits jump the queue.

Leave it running while you work: each new commit is checked as soon as it is made. Stopping and starting again picks
up where it left off.

To look at another repository, run `codist /path/to/repo`. `codist --no-open` skips opening the browser.

## 4. Settings

Put settings in `.codist/config.lua` in the repository. All are optional:

```lua
model = { url = "http://localhost:11434", name = "qwen2.5-coder:7b", context = 8192 }
max_files = 8            -- changed files per commit the model reads; checks read all of them
history = 50             -- recent commits the model reads in the background; 0 for all
ignore = { "target/", "build/", "generated/" }
behaviour = false        -- run changed methods before and after each recent commit (see below)
```

The file is Lua, so a team can compute values instead of copying them; it runs with no file, network or OS access.
Environment variables override it for one run:

| Variable | Meaning |
|---|---|
| `PORT` | the port to serve on (default 8080) |
| `CODIST_MODEL`, `CODIST_MODEL_URL` | model name and server address |
| `CODIST_MODEL_REVIEW=false` | code checks only |
| `CODIST_HISTORY` | how many recent commits the model reads; 0 for all |
| `CODIST_BEHAVIOUR=1` | turn on running changed methods (see below) |
| `CODIST_DATA_DIR` | where Codist keeps its data (default `~/.codist`) |
| `CODIST_LOG` | how much it logs: `debug`, `info` (default), `warn` or `error` |
| `CODIST_LOG_JSON=1` | log one JSON object per line, for log tools |

### Your team's rules

Write the rules your code must keep in `.codist/rules.md`, in plain words: "a coupon and a loyalty discount never
stack", "cancelling releases only what was reserved". The model reads them with every review and reports a change
that breaks one, naming the rule. Keep them short; the first 4,000 characters are used. Codist reads the file fresh
for each review, so edits apply to the next commit it reads.

## 5. Using it

- **Review** lists commits newest first. Pick one to see its diff with issues next to the lines. Issues from code
  checks are exact; issues from the model show the line it quoted, and claims Codist could not confirm are shown as
  notes. Mark an issue resolved by hand, or let the commit that fixes it close it.
- **Fix**: on a real bug, press **Fix** to get a prepared change with the reason, the current and proposed code side by
  side, and whether it still builds and passes the tests. **Apply & commit** commits just that fix; **Suggest how to
  fix it…** asks for it your way; **Reject…** with a reason is remembered for later fixes.
- **Activity** shows the three lanes (code checks, AI model, behaviour runs) with what runs now, what waits and what
  finished, every model call with the exact prompt and reply, and the live log. Use it to see why a review is slow.
- **Graph** draws services, then classes, then methods, with Feign calls between services. Pick a commit to see
  what it changed and what depends on that. Right-click a class for its blast radius.
- **Incidents**: paste logs from a failing service. Codist finds each Java error, the line in your code, the commit
  that last changed it, and a short list of suspect commits; with a model it suggests causes and checks every line
  it quotes.
- **Repository**: modules and their ports, routes and who calls them, hotspots that change often and have open
  issues, and methods that may be slow.

- **Behaviour**, when turned on: for each recent commit, Codist builds the code before and after it, calls the
  methods it changed (and the methods that call them) with the same inputs, and lists every call whose result
  changed: "before: 1200, after: 1201". You decide whether each change was meant. A commit that only adds code, or
  that it cannot build, says why it was not run.

The branch picker in the top bar shows any branch, read from git, without changing your checkout.

## Behaviour: running the changed code (optional)

Turn it on per repository with `behaviour = true` in `.codist/config.lua`, or `CODIST_BEHAVIOUR=1` for one run. It
needs a JDK 17 or later (`java` and `javac`) and, for Maven projects, the project's `mvnw` or `mvn`; Gradle is not
supported yet. It is off by default because it runs your project's code. Codist runs it in a scratch folder with an
environment cut down to the path, the JDK and the locale, so keys and passwords in your environment do not reach it.

Inputs are standard edge cases for each parameter type, so it finds differences in logic that takes plain values
best. A service that needs a database or another service is not covered yet.

## Common problems

**"no model server at http://localhost:11434"**: start Ollama (open the app, or run `ollama serve`), or set
`model.url` to where your server runs. Codist checks again every 30 seconds.

**"qwen2.5-coder:7b is not on the server"**: run `ollama pull qwen2.5-coder:7b`, or set `model.name` to a model you
have (`ollama list`).

**"cannot listen on 127.0.0.1:8080"**: something else uses the port. Run `PORT=8090 codist`.

**macOS says the program cannot be opened**: the installer clears the download mark; if you copied the file by hand,
run `xattr -d com.apple.quarantine ~/.local/bin/codist`.

**Reviews are slow**: that is the model. Use `qwen2.5-coder:7b`, lower `history`, or set `max_files` smaller. Code
check results show at once either way.

**Start over**: stop Codist and delete `~/.codist/<repo>.db`.

`codist doctor` runs the same checks as the installer at any time.
