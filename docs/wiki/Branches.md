# Branches

## Look at any branch in one click

The branch name in the top bar is a switcher. Pick a branch and the commits, graph and issues all show that branch.
Nothing is checked out and no file changes. While you view a branch that is not checked out, Codist reads it from git
and shows **read-only**: fixes and commits are off, because they would land on whichever branch is checked out.

A `git checkout` in your terminal is followed automatically, unless `CODIST_BRANCH` pins a branch.

The server returns to the checked-out branch when it restarts.

## Branches screen

Every branch against the tracked one and against each other, in parallel, using `git merge-tree`, so nothing is touched.

- **Conflict:** these branches will conflict when merged, with the files and the people involved.
- **Overlap:** they change the same files but merge cleanly today.

## Rewording commits

The **Commit** tab suggests a better message and rewrites the commit in one click, keeping its code and author and
replaying later commits. Commits that are already pushed are refused, because rewriting them would change history for
everyone.
