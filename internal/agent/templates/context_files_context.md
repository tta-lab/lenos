---
order: 20
---
{{if .ContextFiles}}Read key instructions.
```bash
{{range .ContextFiles}}cat {{shellQuote .Path}}
{{end}}```
{{end}}
