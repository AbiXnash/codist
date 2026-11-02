# Direction and roadmap

Codist makes a developer's work easier with an AI that runs locally. Code stays on the machine. Rules and analysers
teach the model the standards and give it verified facts. The model reads the code and recent commits, reasons over
those facts and prepares actions, and the developer approves them.

## Decisions

| Question | Decision | Why |
|---|---|---|
| Where does the model run? | Local and offline | Privacy is the point. A larger model on your own server can come later, through the same Ollama interface. |
| Pretrained or trained? | Pretrained with retrieval first, fine-tune later | There is too little verified data to train on yet. |
| Languages? | Java first (Spring Boot, Maven, Gradle) | Depth over breadth. |
| How much autonomy? | Prepare patches, the developer approves | Fixes are not reliable enough to apply unattended. Measure before changing this. |

## Principles

- Tools discover facts; the model reasons over them. It should not have to find everything itself.
- A rule's finding is reported even if the model disagrees.
- Report what is confirmed separately from what is only possible.
- Train only on reviewed and verified examples, never on every suggested patch.
- Every model, prompt or training change has to win on `eval/` before it stays.

## Considered and not planned for now

| Idea | Why not now |
|---|---|
| Rewrite with Python and a separate Java engine | Would lose the single binary, and the Go server and tree-sitter already do this work. |
| Neo4j, Qdrant, PostgreSQL | Each is a server to install. In-memory graphs and BM25 work at this scale. Revisit if search quality or repository size demands it. |
| MCP | Codist is not exposing tools to outside agents, and the tools run in-process. Add it if another AI client needs them. |
| Multi-agent workflow | One model call at a time on a laptop. Separate prompts cost time and have not shown a gain. |

## Next

1. **Standards brief:** turn rules and Memory into a short brief for each file, and measure whether the model finds more.
2. **Verify fixes:** compile a fix and run the tests that reach it before showing it, using the impact report to pick them.
3. **Whole-repository scan:** a resumable background scan with visible progress, most-changed files first.
4. **Action queue:** what the model recommends, ready to approve.
5. **Security tools:** Semgrep and OWASP Dependency-Check as optional analysers, since they run locally. CodeQL has licence limits to check first.
6. **Safe refactors:** OpenRewrite recipes for mechanical changes, instead of asking the model.
7. **Wider evaluation:** add fix success, regression rate and impact accuracy to `eval/`, with bugs from real projects.
