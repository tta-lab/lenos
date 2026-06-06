Inspect local review state.
```bash
DEFAULT=$(git rev-parse --abbrev-ref origin/HEAD 2>/dev/null || echo main)
git diff --stat $DEFAULT...HEAD
git log --oneline $DEFAULT...HEAD
```
