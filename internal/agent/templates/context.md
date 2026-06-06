List registered projects and available skills.
```bash
project list
skill list
```

---

{{if .ContextFiles}}Read key instructions.
```bash
{{range .ContextFiles}}cat {{shellQuote .Path}}
{{end}}```
---

{{end}}
Ready.

Lets rock and roll.
