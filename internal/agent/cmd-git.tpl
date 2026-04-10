{{- if .IsGitRepo -}}
## Git

Working directory is a git repository.
{{if .GitStatus}}

Git status (snapshot at conversation start - may be outdated):
{{.GitStatus}}
{{end}}

Common operations:
  git status               # full status
  git status --short       # compact
  git diff                 # unstaged changes
  git diff --cached        # staged changes
  git diff HEAD            # all changes vs HEAD
  git branch               # list branches
  git switch <branch>      # switch branches
  git add <file>           # stage file
  git commit -m "message"   # commit
  git push origin <branch> # push

## Tips
- Use `git grt` (git root) before git operations in monorepos to ensure you're at the root.
- Use `git log --oneline -n 5` to see recent commits.
- Use `git stash` to temporarily shelve changes.

{{.Attribution}}
{{- end -}}
