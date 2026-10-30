# Codist review knowledge

Each YAML file is a list of rules. For every changed file, Codist picks the rules whose `triggers`
match the change and gives them to the model as a checklist, with the bad and good examples.

```yaml
- id: unique-kebab-id
  title: Short name of the problem
  category: bug | concurrency | spring | jpa | security | performance | design | logging | testing | config
  severity: critical | high | medium | low | info
  triggers: ['@Transactional', 're:(?i)regex']   # substring, or regex after "re:"; empty = general rule
  applies: [java]            # java (default), yaml, properties, xml, sql
  paths: ['src/test/']       # optional: only files whose path contains one of these
  check: What a reviewer looks for.
  why: What goes wrong in production.
  bad: |
    code that has the problem
  good: |
    the fix
```

Add your own files to `~/.codist/rules/` or to `.codist/rules/` inside the repository under review.
A rule with the same `id` as a built-in one replaces it.
