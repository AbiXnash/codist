# Working on Codist

## Set up

You need git and Go 1.26 or later; Node 20 or later only to change the interface. One script clones all eight
repositories side by side and ties the Go modules together:

```bash
git clone https://github.com/AbiXnash/codist.git
sh codist/scripts/dev-setup.sh             # macOS and Linux
```

```powershell
git clone https://github.com/AbiXnash/codist.git
.\codist\scripts\dev-setup.ps1             # Windows
```

You get:

```
codist/          the app, with go.work pointing at the folders below
codist-java/
codist-git/
codist-model/
codist-review/
codist-behaviour/
codist-ui/
codist-eval/
```

`go.work` makes the app build with your local copies of the modules instead of their published versions, so a
change in `codist-java` shows up in the app at once. It is ignored by git.

## Everyday commands

In `codist/`:

```bash
make test                              # the app's tests
make build && ./bin/codist ../some-repo
make dev REPO=../some-repo             # server on :8080, interface with live reload on :5173
```

In any module:

```bash
go test -tags 'grammar_subset grammar_subset_java' ./...
```

The tags keep only the Java grammar in the parser; every package that reaches `codist-java` needs them.

The interface, in `codist-ui/`:

```bash
npm run dev       # needs Codist running on :8080 (CODIST_API to change)
npm run build     # rebuilds dist/; commit dist/ with your change, since Go embeds it
```

## Tests

Tests run against a real git repository that `codist-git/testrepo` builds: two Spring services where
order-service calls payment-service through Feign, one commit adds an empty catch, the next fixes it, and a branch
is one commit ahead. The app's tests start the real server with a fake model server and go through every API route.
Coverage is above 90% in every module except the app's `main` (85%) and settings (88%).

## Logs

Every module logs with `log/slog` through `slog.Default()`, so the app sets the level and format once in `main.go`
(`CODIST_LOG`, `CODIST_LOG_JSON`) and the libraries follow it. Each library line carries `lib=<name>` (git, java,
model, review, risk, store, behaviour, fix). The levels mean:

- **error**: Codist cannot go on (the server stopped).
- **warn**: something failed and Codist carried on without it, such as a save that did not stick, a model reply that
  was not JSON, or a file that could not be read. These are worth a look.
- **info**: one line per finished piece of work: a commit reviewed, an incident read, a fix prepared or applied.
- **debug**: each step: every git command with its time, each model call with its tokens and speed, each dropped
  model finding and why, Maven and javac failures in full, and every API request.

Run with `CODIST_LOG=debug` when something looks wrong. A library used outside Codist logs through whatever
`slog.Default()` that program sets.

## Changing a module

1. Make the change in the module and its tests, and in the app if it uses it; `go.work` builds them together.
2. Commit and push the module, then tag a new version: `git tag v0.2.1 && git push origin v0.2.1`.
3. In the app, require it: `GOWORK=off go get github.com/AbiXnash/codist-java@v0.2.1 && go mod tidy`, then check
   `GOWORK=off make test` passes without your local copies, and commit `go.mod` and `go.sum`.

Tag modules before the app, and dependencies before what uses them: codist-git and codist-model, then codist-java,
then codist-review and codist-behaviour, then codist.

## Releases

Pushing a tag such as `v0.1.0` to this repository runs `.github/workflows/release.yml`: it tests, builds one file
each for macOS (Apple silicon and Intel), Linux (x64 and arm64) and Windows, and attaches them to a GitHub release.
The install scripts download from the latest release.

Every push also runs `.github/workflows/ci.yml`: the tests on Linux, macOS and Windows, both install scripts, and the
development setup.
