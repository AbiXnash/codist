# Screens

The top bar groups the screens by what you are doing.

| Group | Screens |
|---|---|
| Code | **Review**, **Repo**, **Branches** |
| People | **Mine**, **Team** |
| Ship | **Release**, **Incidents** |
| AI | **Activity**, **Memory** |

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

## Repo

The repository as it is: services with ports, tests, endpoints and who calls whom, API endpoints, Feign calls, people,
hotspots, costly methods, largest classes and constants.

## Mine and Team

**Mine** is your to-do list by urgency, your commits and what you fixed. **Team** shows per person what is open, assigned
and fixed, where they work, and which urgent issues nobody has taken.

## Activity

Every model call as it happens: what was fed to the model section by section, its instructions, its response, tokens,
time, and the commit, file and knowledge entries it relates to. Calls saved by reusing an identical answer show here too.

## Keyboard

| Key | Action |
|---|---|
| `/` or `⌘K` | Go to class |
| `[` / `]` | Hide or show the history / right panel |
| `j` / `k` | Next / previous commit |
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
  (controllers on top, then services, repositories, entities), **Services** (one box per service) and **Hubs**; size by links, code
  size or open issues; labels; package boxes; PNG export.
- **Right-click a class**: blast radius (what depends on it, by distance), show only its neighbours,
  dependencies or dependents, path from or to it, hide it, open its code.
- **Analysis**: dependency **cycles**, **layer violations** (upward dependencies, controllers using
  repositories directly), **blast radius**, **path** between two classes, and **hotspots** (most depended
  on, fragile, largest). Hover a class for fan-in, fan-out, instability and size.
- **What does it all mean?** (the ? button) explains every shape, letter, ring, line and colour.
