Inspect local review state.
```bash
# Each Lenos run block is ephemeral; variables set here will not exist in
# future calls.
git diff --stat $(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null || echo main)...HEAD
git log --oneline $(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null || echo main)...HEAD
```
