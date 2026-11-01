# Incidents

Root cause analysis from logs.

## Incidents: root cause analysis from logs

Try it on `~/dev/rca-demo` (a Spring Boot app calling an unreliable quote service; its log fills with real
NullPointerExceptions). See that folder's README.

Open **Incidents** in the top bar, paste the logs from an incident (or drop a log file) and click **Find root cause**.
The logs are analysed on this machine and never sent anywhere.

1. **Errors.** Stack traces, `Caused by` chains and ERROR lines are parsed; repeats are merged and counted.
2. **Code.** Frames in the project are mapped to classes, methods and lines through the code graph, with
   `git blame` for each line and any open Codist issue nearby.
3. **Suspect commits.** Recent commits that touched the failing code are ranked. Strongest signals: Codist flagged a
   severe issue there in that commit; the commit last changed a field the error names ("this.customers is null");
   it changed the failing method; it last changed the failing line.
4. **Cause.** The local model explains the most likely causes, each with log and code evidence and a confidence.
5. **Act.** Confirm the root cause, then: **Propose fix** (an exact tracked fix when one exists), **Regression test**
   (a test that fails while the defect exists; dependency-injection defects get an exact Spring wiring test), and
   **Create review rule**, which saves the pattern to `.codist/rules/learned.yaml` so commit review catches it next time.
6. **Report.** An editable RCA report (summary, timeline, impact, root cause, fix, prevention) to copy as Markdown,
   and a runbook draft for the support team that can be committed under `docs/runbooks/`.

Past incidents are searched for similar ones (same exceptions, frames and messages) and their confirmed causes are
shown and given to the model.
