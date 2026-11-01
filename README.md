# Codist

A local code reviewer for Java and Spring Boot repositories. It watches a git repo, draws a live graph of your classes
and services, reviews every commit with rules, static analysers and a local LLM, tracks issues across commits, and
proposes fixes you approve. Nothing leaves your machine.

## Run

**One binary.** Build it once, then run it from inside any git repository. The browser opens by itself.

```bash
make install-bin       # needs Go, a C compiler and Node, only to build it
cd ~/dev/your-spring-app
codist                 # or: codist /path/to/repo
```

`make dist` writes a single file you can copy to other machines of the same OS and CPU, which need only git.

**The AI part is optional.** Without it, Codist still runs its rules and analysers. To add it, install
[Ollama](https://ollama.com), then:

```bash
codist setup           # downloads the right model for this machine
codist doctor          # shows what is installed and what is missing
```

**Docker** needs nothing else installed, but runs the model without the GPU on a Mac, so it is much slower there:

```bash
REPO=~/dev/your-spring-app docker compose up
```

## Docs

Everything else is in the [wiki](docs/wiki/Home.md): [setup](docs/wiki/Setup.md),
[screens](docs/wiki/Screens.md), [issues](docs/wiki/Issues.md), [fixing](docs/wiki/Fixing.md),
[branches](docs/wiki/Branches.md), [knowledge and memory](docs/wiki/Knowledge.md), [release](docs/wiki/Release.md),
[incidents](docs/wiki/Incidents.md), [configuration](docs/wiki/Configuration.md), [feature flags](docs/wiki/Feature-flags.md) and
[architecture](docs/wiki/Architecture.md).
