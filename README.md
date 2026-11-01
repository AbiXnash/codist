# Codist

A local code reviewer for Java and Spring Boot repositories. It watches a git repo, draws a live graph of your classes
and services, reviews every commit with rules, static analysers and a local LLM, tracks issues across commits, and
proposes fixes you approve. Nothing leaves your machine.

## Run

You need Go 1.22+, a C compiler, Node 20.19+ or 22.12+, git and [Ollama](https://ollama.com).

```bash
make install
ollama pull qwen2.5-coder:7b
make run REPO=~/dev/your-spring-app   # http://localhost:8080
```

Optional, for better findings: Java 21 and Maven (PMD, SpotBugs), and `brew install jdtls` (the Eclipse Java language
server).

## Docs

Everything else is in the [wiki](docs/wiki/Home.md): [setup](docs/wiki/Setup.md),
[screens](docs/wiki/Screens.md), [issues](docs/wiki/Issues.md), [fixing](docs/wiki/Fixing.md),
[branches](docs/wiki/Branches.md), [knowledge and memory](docs/wiki/Knowledge.md), [release](docs/wiki/Release.md),
[incidents](docs/wiki/Incidents.md), [configuration](docs/wiki/Configuration.md) and
[architecture](docs/wiki/Architecture.md).
