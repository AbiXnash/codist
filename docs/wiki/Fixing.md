# Fixing issues

How one-click fixes work, what is checked, and where they stop.

## The fix dialog

The dialog shows why the change is proposed, old and new side by side (or unified), and the commit message.

Cancel or Esc stops the model and says so. Clicking outside does nothing while a fix is being generated or shown.

**Reject** takes an optional reason. A reason goes into Memory and is added to later reviews and fixes.

## What is checked before you see a fix

Every model attempt must parse as Java, keep most of the file, import only classes that exist, not break other files,
and change more than whitespace. A real class in the wrong package gets its import corrected. For a class that does not
exist, the model is shown its own attempt and the project's real classes of that sort.

These checks cannot prove a fix solves the problem. A small local model can return a valid change that misses the point,
so the dialog warns when the flagged line is unchanged. Read the diff before applying.

## Duplicate model calls

Identical requests make one call: a running call is joined, and an answer from the last 15 minutes is reused. Calls run
at temperature 0 or close to it, so asking again gives the same answer. To get a different one, change the prompt with
the suggestion box.

## Fixing and editing from the UI

- **Fix** on a finding asks the fix model for a corrected file, checks that it still parses and has not lost
  code, and shows the diff. **Apply & commit** writes the file and commits *only that file* to the current
  branch (other uncommitted work is left alone); the new commit is reviewed automatically. Nothing is pushed.
- **Ask** answers questions about the open file or selected class, and **Change code** turns an instruction
  ("add retry with backoff to the gateway call") into the same reviewed diff.
- Fixes need a local checkout (`CODIST_REPO`), and refuse to run if the file changed since the review.

## Instant fixes

Fixes for these rules are mechanical, so they open immediately and never involve the model: missing injection
(adds a constructor), `System.out` (adds an SLF4J logger and imports), a literal that repeats a constant, a
duplicated log line, a log-only helper method, and a constant borrowed from another domain. Model fixes for the
other issues are prepared in the background after each review (marked ⚡), so they also open instantly. Fixes work
on the working tree as it is now: each place is found again by its code, and places already fixed are skipped.

## Fix with a suggestion

Every open issue has **Suggest how to fix it**: describe the fix you want ("constructor injection, keep the
field name") and the fix follows it. The suggestion must be about that issue: naming another class of the
project is refused at once, and a model check refuses other requests (features, other bugs, chat) with the
reason, before any fix is generated.

## Commit messages

Generic messages ("chore: added logs", "update", "fix") and `chore`/`docs`/`style` commits that change production
code are flagged, with a suggested Conventional Commits message that describes what the diff actually does.
