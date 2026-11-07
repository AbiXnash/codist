# AI engineering in Codist

How the ideas in Chip Huyen's *AI Engineering* (O'Reilly, 2025) apply to Codist: what is built, what was measured,
and what comes next. Sources: the book's public chapter summaries and companion repository
([chiphuyen/aie-book](https://github.com/chiphuyen/aie-book)) and the book itself.

## The stance

The book's central advice is to build around the model's probabilistic output and to set up evaluation early,
combining several methods because no single one is enough. Codist follows it in four ways:

1. **Exact checks before AI judgement.** Fifteen code checks report what can be known for certain; the model only
   adds to them.
2. **No AI judge where an exact check exists** (chapter 3: AI judges drift and their scores are not comparable).
   Each model claim must quote code near the line it names; a quote that is not there is dropped, and a claim the
   code does not back is shown as a note.
3. **Behaviour over opinion.** Running the changed methods before and after a commit gives a fact to show,
   "this returned 1200 and now returns 1201", not a guess.
4. **A private benchmark.** Public benchmarks only filter out weak models (chapter 4); Codist is measured on real
   Java bugs in [codist-eval](https://github.com/AbiXnash/codist-eval), and a change ships only if it helps there.

## What is built

| Book (chapter) | In Codist |
|---|---|
| Evaluation-driven development (3, 4) | `codist-eval`: 118 real bugs, a 10-bug sample for quick paired comparisons, refactoring commits to count false alarms. Every model, prompt or retrieval change is compared on the same bugs. |
| Exact evaluation over AI judges (3) | Quote checks on every model claim; null claims checked against where the value can become null. |
| Structured outputs (5) | The model must answer in a JSON schema with a line and a quoted line of code for each finding. |
| Defensive prompting (5) | The instructions say the commit message, diff and code are material to review, never instructions; text that tries to steer the reviewer is itself reported. The quote checks limit what an injected instruction could do. |
| Context construction (6) | The model sees the diff, the changed file (or, for long files, the changed methods with the class header) and what the code checks already found. |
| Guardrails (10) | Output schema, quote checks, severities lowered for unbacked claims, and a person approves every change. Behaviour runs use an environment without secrets. |
| Prompt and model lineage (4, 10) | Every review records a prompt version, a fingerprint of the instructions and reply format, and the model. |
| Inference metrics (9) | Every review records prompt and reply tokens, model time, prefill (time to the first token) and decode (time per token). The status shows seconds per commit and tokens per second. |
| Prompt caching (9) | Each reply is cached by model, prompt version and exact prompt, so the same change reviewed again (a rebase, a re-run, another branch) costs nothing. |
| User feedback and the data flywheel (10, 8) | Every finding can be marked useful or wrong. A wrong one is set aside and not reported again; both kinds are kept, and become labelled examples for evaluation and, later, for tuning. |
| Observability (10) | Review notes say what was skipped and why; behaviour runs say why a commit was not run; failures name their cause. |
| Workload-aware serving (9) | Code checks never wait for the model; the model reads the newest commits first and only the last 50 by default, since a laptop model takes minutes per commit. |

## Measured and not shipped

**Callers as context (chapter 6).** Showing the model the code in other files that calls the changed methods was
measured on the 10-bug sample with `qwen2.5-coder:7b`: located 5 of 10 without callers, 4 of 10 with. No gain, so it
stays an option (`Reviewer.Callers`) and is off. The reason it did not help is likely that most of these bugs sit
inside one method; it may still matter for service code, which the sample lacks.

## Next, in order

1. **Use the feedback.** Show the model a few of this team's findings marked useful and wrong as examples
   (in-context learning, chapter 5), and measure on the sample whether false alarms drop.
2. **A small risk ranker.** Train a small classifier on the feedback and the history (files that changed often and
   had issues) to decide which commits the model reads first, so its scarce time goes where bugs are likeliest.
3. **A model router** (chapter 10). Send cheap work, such as incident summaries, to a small model and reviews to the
   largest the machine can run.
4. **Self-consistency on serious findings** (chapter 2). Ask twice for critical and high findings and keep those both
   answers agree on; measure the false-alarm cost against the extra time.
5. **Finetuning, last** (chapter 7). Only when there are a few hundred labelled findings from feedback; LoRA on the
   local model, evaluated on the frozen holdout before use.
