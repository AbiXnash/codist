---
description: Sent back to the fix model when its fix added a constant that duplicates an existing one.
format: text
vars: Items: the duplicates
---
## Your previous attempt added duplicate constants
{{range .Items}}- {{.}}
{{end}}
Reuse the existing constant instead of adding a new one with the same value.
