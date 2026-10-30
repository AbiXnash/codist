---
description: Sent back to the model when its regression test is wrong, for example it asserts the defect.
format: text
vars: Items: what is wrong
---
## Your previous test was wrong
{{range .Items}}- {{.}}
{{end}}
Write it again.
