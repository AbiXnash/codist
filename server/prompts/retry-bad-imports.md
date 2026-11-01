---
description: Sent back to the model when its file imports project classes that do not exist.
format: text
vars: Items, the bad imports; Candidates, real project classes of the same sort
---
## Your previous version imported classes that do not exist: {{join .Items ", "}}
You can only change this file, so you cannot add new classes. Do not use those again.
{{if .Candidates}}These classes do exist in the project, use one if it fits: {{join .Candidates ", "}}
{{end}}Otherwise use a class from the JDK or a library the file already uses, such as IllegalStateException or IllegalArgumentException. Types in the same package need no import.
