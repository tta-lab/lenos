{{- if .IsGitRepo -}}
Working directory is a git repository.

Do not assume the current git state from this prompt. When git state matters,
inspect it with bash first, for example `git status --short`, `git diff`, or
`git log`.

{{.Attribution}}
{{- end -}}
