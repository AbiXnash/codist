# Release

Whether the repository is ready to ship.

## Release risk report

**Release** judges the whole repository as it is now on the followed branch, not the commits that led to it. Every Java file is checked with the rules and the knowledge base, the model's tracked
findings are added where their code is still there, and duplicates are grouped by root cause. The report shows every
open issue with who wrote the line and in which commit, issues per module, build problems, which source files the
model has never reviewed, and an overall risk with reasons. Copy it all as Markdown for the release
ticket. Below it, the dependency watch lists
every dependency in the build file with known-vulnerable versions, dev-only libraries on the production classpath,
pinned versions that override Spring Boot's, and end-of-life Spring Boot lines.

Not included: ticketing (Jira, ServiceNow), monitoring alerts and pull-request comments, which need integrations.
