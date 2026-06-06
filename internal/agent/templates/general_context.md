---
order: 10
optional: true
---
List registered projects and available skills.
```bash
project list
skill list
```

{{if .ContextFiles}}---
order: 20
---

Read key instructions.
```bash
{{range .ContextFiles}}cat {{shellQuote .Path}}
{{end}}```
{{end}}
