> The `rebuild` branch has a new menu: Review, Graph, Incidents, Knowledge (formerly Memory) and Activity, with the other screens under More. Review now shows the commit's diff, with syntax highlighting and issues inline, and the panel on the right holds what the diff doesn't: the summary, what the change can affect and the commit message. The graph has its own screen, with Microservice, Service, Layer and Method zoom buttons. In a repository with one module, Microservice splits it by top-level package. Method shows the classes a commit changed and the classes they call or are called by, with a line for each method call: solid when confirmed, dashed when matched by name only. Drag the edge of the commits panel or the right panel to resize it, or focus the edge and use the arrow keys. Codist remembers the width.

# Screens

The sidebar groups the screens by what you are doing. Collapse it to icons with `\` or the logo.

| Group | Screens |
|---|---|
| Code | **Review**, **Repository**, **Branches** |
| People | **My work**, **Team** |
| Ship | **Release**, **Incidents** |
| AI | **Activity**, **Memory** |

The model's status and the appearance switch sit at the bottom of the sidebar.

## Review

- **Left:** **Commits** and **Explorer** share one dock. Commits lists history with each commit's open, closed and
  fixed issues, with search and filters. Explorer holds graph search, shape, grouping, categories, services and
  relations, and shows a class's methods, constants and costs when you select it.
- **Centre:** the code graph.
- **Right:** **Issues**, **Commit** (its review and changes) and **Code**, with **Ask AI** set apart.

## Grouping by service

**Group by: Service** draws a named box per service. Boxes sit apart, with Feign calls as cyan links and other uses as
dashed links. Distance shows relation: more calls between two services brings them closer. In a repository with one
module, the boxes are its top-level packages instead.

## What a change can affect

The **Commit** tab lists each changed method and what depends on it, following method calls through interfaces and Feign
clients, across services: callers, the API routes they serve, and the tests to run. It warns when a method's signature
changed, when a method was removed, and when no test reaches a changed method. The same facts go to the model, so it
checks whether those callers still work.

A caller is **possible** when it matched by method name only. It comes from the code alone, so reflection, events and
configuration can add callers it cannot see.

## Repo

The repository as it is: services with ports, tests, endpoints and who calls whom, API endpoints, Feign calls, people,
hotspots, costly methods, largest classes and constants.

## My work and Team

**My work** is your to-do list by urgency, your commits, what you fixed, and your lines written, removed and still in the code. **Team** shows per person lines and files as well as what is open,
assigned and fixed, where they work, and which urgent issues nobody has taken.

### Developer profiles

Pick a person on **Team**, or scroll down on **My work**, for what git and the reviews show about their work on this
branch: lines, the kind of code they write, where they work, bugs from their commits, fixes and commit habits.

**Does well** and **To work on** come from fixed rules, and each quotes its numbers and links to examples. The same
problem in two or more commits counts as a habit, and gets the fix suggestion as advice.

With fewer than 5 reviewed commits or 200 reviewed lines, a profile only reports facts and draws no conclusions about
quality. People are compared with the team average only when at least two have enough evidence. Nobody is scored or
ranked, because lines do not measure effort and hard code produces more findings.

## Activity

Every model call as it happens: what was fed to the model section by section, its instructions, its response, tokens,
time, and the commit, file and knowledge entries it relates to. Calls saved by reusing an identical answer show here too.

## Keyboard

| Key | Action |
|---|---|
| `/` or `⌘K` | Go to class |
| `\` | Collapse or expand the sidebar |
| `[` / `]` | Hide or show the left dock / right panel |
| `j` / `k` | Next / previous issue on Review; next / previous commit elsewhere |
| `J` / `K` | Next / previous commit |
| `1`–`5` | Issues, commit review, commit changes, Code, Ask |
| `⌘↵` | Commit the fix in the fix dialog; "change code" in the Ask tab |

## Uncommitted changes

While you edit (local checkout), Codist runs the rules and the knowledge base on the lines you changed but have
not committed, in milliseconds and without the model. They appear at the top of History as **Uncommitted
changes** and in the Issues hub's **Uncommitted** scope; the graph marks the types you are editing. Fixes there
are applied to the working tree and **not committed**, so you commit them together with your own change. Issues
your edit already fixes show as "fixed locally".

Some findings are heads-ups rather than defects, such as "shared constant changed": they have **Mark as checked**
instead of Fix, because a fix could only undo your change.

The row expands into a recommended plan: how many commits to make, which files go in each, a message you can edit or
ask the AI to write, and a Commit button that commits only those files.

### The graph

- **Controls** (top right of the graph): find by class, package or `@Annotation` (highlight or show only
  matches); show or hide roles, link kinds, tests and packages; shapes **Graph**, **Layers**
  (controllers on top, then services, repositories, entities), and **Services** (one box per service); size by links, code
  size or open issues; labels; package boxes; PNG export.
- **Right-click a class**: blast radius (what depends on it, by distance), show only its neighbours,
  dependencies or dependents, path from or to it, hide it, open its code.
- **Analysis**: dependency **cycles**, **layer violations** (upward dependencies, controllers using
  repositories directly), **blast radius**, **path** between two classes, and **hotspots** (most depended
  on, fragile, largest). Hover a class for fan-in, fan-out, instability and size.
- **What does it all mean?** (the ? button) explains every shape, letter, ring, line and colour.
