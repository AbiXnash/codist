# Feature flags

Turn menus and features on and off with a JSON file. Anything the file leaves out stays on, so it only needs to list what
you want off. For example, this hides the team-facing screens and AI fixes:

```json
{
  "menus": { "team": false, "incidents": false },
  "features": { "fix": false }
}
```

Save it, then refresh the page. No restart is needed. [`features.example.json`](../../features.example.json) lists
every switch.

## Where the file goes

Later files win over earlier ones.

| Order | File | Use it for |
|---|---|---|
| 1 | `~/.codist/features.json` | Your preference on every repository |
| 2 | `<repo>/.codist/features.json` | This repository; commit it to share with the team |
| 3 | The file named by `CODIST_FEATURES` | One run or one deployment |

## Switches

| Group | Switch | Turning it off |
|---|---|---|
| `menus` | `review`, `repo`, `branches`, `mine`, `team`, `release`, `incidents`, `activity`, `memory` | Hides that screen. Incidents also stops accepting new logs. |
| `panels` | `issues`, `commit`, `code` | Hides that tab in the right panel |
| `panels` | `ask` | Hides Ask AI and refuses its API |
| `features` | `explorer` | Hides the Explorer tab |
| `features` | `branchGraph` | Hides the Branch graph |
| `features` | `branchSwitcher` | Shows the branch name without the switcher and refuses switching |
| `features` | `fix` | Removes the Fix buttons and refuses fixes |
| `features` | `reword` | Removes suggesting and rewording commit messages |
| `features` | `localPlan` | Removes the recommended commits for local changes |
| `features` | `issueWorkflow` | Removes Start, In review and the stage display |

## Mistakes

A typo or broken JSON never stops Codist. The rest of the file still applies if only a name is wrong, a broken file is
skipped, and either way a message appears in the UI. `GET /api/features` shows the result and which files were used.

These switches are for tailoring the interface. They are not access control: anyone who can edit the file or reach the
machine can change them.
