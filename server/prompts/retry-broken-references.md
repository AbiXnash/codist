---
description: Sent back to the fix model when its fix removed or renamed members other files still use.
format: text
vars: Items: what broke
---
## Your previous attempt broke other files
{{range .Items}}- {{.}}
{{end}}
Keep those members exactly as they are and add new ones instead.
