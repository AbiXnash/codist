---
description: Sent back to the model when its file imports project classes that do not exist.
format: text
vars: Items: the bad imports
---
## Your previous version imported classes that do not exist: {{join .Items ", "}}
Use the project's real packages (types in the same package need no import).
