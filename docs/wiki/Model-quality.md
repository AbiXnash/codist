# Model quality

Run the evaluation before believing any claim about a model, a prompt or a fine-tune, including the ones here.

```bash
make build
python3 eval/run.py --rules-only                         # no model
python3 eval/run.py qwen2.5-coder:7b                     # one model
python3 eval/run.py qwen2.5-coder:7b qwen2.5-coder:3b    # compare
python3 eval/run.py qwen2.5-coder:7b@8192                # with a context size
python3 eval/run.py MODEL --no-rules --no-filters        # what the model finds alone
python3 eval/run.py MODEL --no-rag                       # what retrieval earns; also --no-facts
python3 eval/run.py MODEL --repeat 3                     # several runs of each
python3 eval/run.py MODEL --default-settings             # the settings users get, not the eval's
python3 eval/run.py --compare A.json B.json              # paired test between two results
```

It reviews `eval/project`, a small Spring application with known bugs listed in `eval/labels.json`, on a temporary copy.
It reports bugs found, logic bugs found, findings that match nothing (noise), false alarms on clean files, model calls
and time. Add bugs from your own code to `eval/` to make it reflect your work.

Each result file records what is needed to reproduce it: the commit, binary, prompt, label and project hashes, the
model digest, the machine and the settings. It also records the time of every model call and every model finding the
filters dropped, with the reason. Scores print with a 95% range, because a test this small can't separate close
results. A model run in which the model was never called exits with an error instead of giving a score.

`noise` counts findings that match no label. `excuse` counts findings that match no label but are on the `alsoTrue`
list, which earlier versions left out of the report. **This test is 16 hand-seeded bugs in one small project, written
alongside some of the rules it scores, so use it as a smoke test.** Don't quote it as accuracy. A larger test set is
milestone 1 of the [rebuild plan](Rebuild-plan.md).

## What was measured

On a 16 GB Apple Silicon Mac, with the 15 bugs labelled at the time. One run of each, so treat the times as rough.

| Setup | Bugs found | Logic bugs | Time |
|---|---|---|---|
| Rules only | 10 | 1 of 6 | 2 s |
| `7b`, 16k context | 10 | 1 of 6 | 16 min |
| `7b`, 8k context | 10 | 1 of 6 | 12 min |
| `3b`, 8k context | 11 | 2 of 6 | 15 min |

The models also reported one real bug the labels did not list at the time, a race in `MemberService`, now added as a
16th label. The `3b` model made more unmatched reports: 6 against 2.

Two other ways of asking were tried on the logic bugs alone: asking whether each method does what it claims, and making
the model trace each method by hand with concrete values. Neither found them. In the trace, the model wrote down a
"result" that matched what the method should do, without really working it out.

**In those first runs, the rules did most of the work and the small models rarely found logic bugs.** A clean review is not
evidence that the logic is right.

Tested later on six files, with the "does it do what it claims" prompt:

| Model | Result | Time per file |
|---|---|---|
| `qwen3:8b`, reasoning on | Found the `==` bug and the commented-out line, no false alarms on 2 clean files. Timed out or ran out of output on the 2 files with the hardest bugs. | 1 to 5 min |
| `qwen2.5-coder:14b` | Found the commented-out line, no false alarms. | 3 to 24 s |

A sceptical second model call on each finding was also tried, as some cloud reviewers do. It dropped every model finding, including a real race, and doubled the time, so it was removed. Plain checks replaced it: a finding must quote a line that is in the file, and advice that starts "Consider…" is dropped.

## Current score

The default model is now `qwen2.5-coder:14b`, chosen by the project owner. **It has not had the full evaluation yet**, so the `7b` score below is the last measured one.

With facts in the prompt and answers checked, on 16 labelled bugs. The same result repeated across two runs.

| Setup | Bugs found | Logic bugs | Noise | Time |
|---|---|---|---|---|
| Rules only | 10 | 1 of 7 | 1 | 2 s |
| `7b`, current | 12 | 3 of 7 | 1 | 3.5 to 4.3 min |

Noise counts every finding the labels do not cover, including info. Still missed: an inverted condition, a wrong boolean, and two ISBN validation bugs.

## Faster reviews

- Files holding only fields and getters no longer go to the model.
- The default context is now 8k, which found the same bugs as 16k in less time.
- Set `CODIST_MODEL_REVIEW=false` for reviews in seconds using rules and analysers only. Fixes and Ask still use the model.
- Identical model requests make one call, and only three fixes per review are prepared in the background.
- Run only one model if memory is tight: two loaded together on 16 GB made some calls take many minutes.

## Better results, most useful first

1. **Add a rule when you find a kind of bug that can be detected.** It is free, instant and never forgets.
2. **Teach it your conventions** in Memory, and reject bad fixes with a reason.
3. **Try a bigger model.** Logic needs reasoning, and that comes with size. `ollama pull qwen2.5-coder:14b` is about 9 GB,
   which is tight on 16 GB. Run the evaluation on it before switching.
4. **Fine-tune last.**

## Training

Fine-tuning teaches a model patterns from examples. `codist export-training data.jsonl` writes about 240 examples from the
rules, which are bugs the rules already catch, so **expect little from training on those alone**. It becomes worthwhile
with several hundred real examples of bugs the model misses, taken from your own reviews and fixes.

On a Mac, the route is [MLX](https://github.com/ml-explore/mlx-lm). **These steps have not been run for Codist**, so check
each tool's current instructions.

```bash
python3 -m venv .venv && . .venv/bin/activate
pip install "mlx-lm[train]"

mkdir -p train
codist export-training all.jsonl
python3 - <<'PY'
import random
rows = open("all.jsonl").readlines()
random.seed(1); random.shuffle(rows)
cut = max(1, len(rows) // 10)
open("train/valid.jsonl", "w").writelines(rows[:cut])
open("train/train.jsonl", "w").writelines(rows[cut:])
PY

mlx_lm.lora --model Qwen/Qwen2.5-Coder-3B-Instruct --train --data train --iters 600 --batch-size 1 --adapter-path adapters
mlx_lm.fuse --model Qwen/Qwen2.5-Coder-3B-Instruct --adapter-path adapters --save-path fused
```

Training works from the original weights on Hugging Face, not the Ollama download. To use the result in Ollama, convert
`fused` to GGUF with llama.cpp's `convert_hf_to_gguf.py`, then create a model from a `Modelfile` with `FROM ./model.gguf`
and the ChatML template that Qwen uses. Ollama's documentation does not list Qwen among the architectures that can
load an adapter directly, which is why the weights are fused first.

Then compare, and keep it only if it wins:

```bash
ollama create codist-java -f Modelfile
python3 eval/run.py qwen2.5-coder:3b codist-java
```
