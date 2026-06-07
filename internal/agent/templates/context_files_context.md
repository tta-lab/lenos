---
order: 20
---
{{if .ContextFiles}}Read key instructions.
```bash
{{range .ContextFiles}}cat {{shellQuote .Path}}
{{end}}```
{{else}}
No project guidance file was found for this workspace.
{{end}}
