{{- if .IsGitRepo -}}
Working directory is a git repository.
{{if .GitStatus}}

Git status (snapshot at conversation start - may be outdated):
{{.GitStatus}}
{{end}}

{{.Attribution}}
{{- end -}}
