# Branches

## Look at any branch in one click

The branch name in the top bar is a switcher. Pick a branch and the commits, graph and issues all show that branch.
Nothing is checked out and no file changes. While you view a branch that is not checked out, Codist reads it from git
and shows **read-only**: fixes and commits are off, because they would land on whichever branch is checked out.

A `git checkout` in your terminal is followed automatically, unless `CODIST_BRANCH` pins a branch.

The server returns to the checked-out branch when it restarts.

## Branch graph

In Review, the centre switches between **Code graph** and **Branch graph**. The branch graph draws every branch, or the
ones you pick, as lanes with their merges. A filled dot is a reviewed commit. Click a commit to review it, even if it
is on another branch. Click a branch name to make commits, code graph and issues follow it.

## Fix a bug on every branch

After you commit a fix, Codist looks for the same code on every other branch. If any still have it, a dialog offers
**Fix all branches**. You can also start this from any commit, including one you fixed by hand, with **Apply to other
branches** in the Commit tab.

| Result | Meaning |
|---|---|
| Apply the fix | The branch has the code the fix removed, and the fix applies cleanly. Selected by default. |
| Needs you | The code around it differs, or the branch is in another worktree or only on the remote. Nothing changes there. |
| Nothing to do | The branch does not have that code, or is already fixed. |

Each branch gets a new commit on top, saying which commit it came from. Nothing is checked out or pushed, and your
working files do not change. To undo one, use `git reset` on that branch; `git reflog show <branch>` lists its earlier
positions. A fix is judged by whether the code is the same, not by testing it, so check each branch builds.

Fixing an issue on one branch closes it only there. Each branch keeps it open until it has the fix too.

## Branches screen

Every branch against the tracked one and against each other, in parallel, using `git merge-tree`, so nothing is touched.

- **Conflict:** these branches will conflict when merged, with the files and the people involved.
- **Overlap:** they change the same files but merge cleanly today.

## Rewording commits

The **Commit** tab suggests a better message and rewrites the commit in one click, keeping its code and author and
replaying later commits. Commits that are already pushed are refused, because rewriting them would change history for
everyone.
