# Bugbase

Knowledge Codist retrieves for the model while it reviews code: real-world Java and Spring defects,
described the way a senior engineer would explain them, not as checklist items. For each changed file,
the entries that most resemble the code are retrieved (BM25 over the code's identifiers, annotations,
calls and types, optionally re-ranked by embeddings) and handed to the model as reference material.

Add your own: drop `*.md` files with the same format into `<data-dir>/bugbase/` (yours) or
`<repo>/.codist/bugbase/` (the team's). Fixed issues and dismissed findings are also written back as
lessons (`<data-dir>/<repo>/lessons.md`), so the reviewer learns from this codebase.

## Format

Each entry is a level-2 heading with an id, a few metadata lines, then prose and examples:

    ## Optional.get() without a presence check {#optional-get-unchecked}
    severity: medium
    category: bug
    tags: Optional get isPresent orElseThrow NoSuchElementException
    see: Sonar S3655

    What happens, why it breaks in production, how it looks in code.

    ```java
    // bad
    ...
    // better
    ...
    ```

`tags` are extra words that help retrieval (API names, exceptions, annotations). `see` names the
equivalent rule in other tools, for reference.
