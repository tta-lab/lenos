# Upstream Integration Workflow

> **This workflow is a skill.** Agents should load `skill get upstream-workflow`
> to get the full instructions inline. The skill also sits at
> `.agents/skills/upstream-workflow/SKILL.md` for direct file reading.

The quick-reference cheat sheet (commit format + provenance + verification)
is kept below in case the skill system isn't available.

---

## Cheat Sheet

### Branch naming

`upstream/<category>/<short-desc>` — e.g. `upstream/db/connection-pool`

### Commit message format

```
chore(upstream): port <hash> — <upstream commit title>
```

### Provenance comment

```go
// Ported from upstream commit 61ee2d2e.
// Original: fix(db): use connection pool to avoid corrupted writes
```

### Audit before porting

```bash
ttal jump crush
git show <hash> --stat -p
ttal jump lenos
```

### Pre-merge verification

```bash
go test ./...                           # all tests pass
grep -rn "Ported from upstream commit" internal/ | grep -v "_test.go"
grep -rn "CRUSH_" internal/ | grep -v "_test.go" | grep -v "docs/" || echo "clean"
```

### PR description

Must contain upstream references table mapping commit → description.

### After merge

- Mark commits `✅ MERGED` in `v0.74-*.md`
- When category complete: `mv v0.74-foo.md v0.74-foo.done.md`
- Remove from README.md checklist
