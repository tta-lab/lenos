# Upstream Integration Workflow

> How we track upstream Crush commits in our fork, ensuring every merged
> change retains provenance for future audits and diff reviews.

---

## 1. Before merging any upstream commit

### 1.1 Identify the upstream commit

```bash
# Show the full commit hash + message
git log --oneline refs/tags/v0.55.0..refs/tags/v0.74.1 -- <files>
# Or for a specific hash:
git show <upstream-hash> --stat
```

### 1.2 Create a tracking PR

Branch naming convention: `upstream/<category>/<short-desc>`.

Examples:

| Branch | Purpose |
|---|---|
| `upstream/db/connection-pool` | Port `61ee2d2e` |
| `upstream/db/txlock-corruption` | Port `40108413` |
| `upstream/agent/token-accounting` | Port `6ed8852b` + `2e9c6505` + `74e6e378` |
| `upstream/agent/env-harden` | Port `c2be8cbf` |
| `upstream/pubsub/buffer` | Port `0585f498` |
| `upstream/config/path-scope` | Port `e2e0bc09` + `e1123687` + `79b2d619` |

---

## 2. PR description format

Every tracking PR description MUST contain:

```markdown
## Upstream references

| # | Commit | Date | Description |
|---|---|---|---|
| 1 | `61ee2d2e` | 2026-05-11 | fix(db): use connection pool to avoid corrupted writes |
| 2 | `40108413` | 2026-05-04 | fix(db): prevent SQLITE_NOTADB corruption |

## Changes

- Port `61ee2d2e`: added `db.SetMaxOpenConns(1)` after migration setup
- Port `40108413`: appended `_txlock=immediate` to DSN, switched ncruces DSN to file: URI prefix

## Verification

- [ ] `go test ./internal/db/...` passes
- [ ] Manual: create session, run a few turns, verify no corruption
```

The table is required — it's the only place that maps one PR to multiple
upstream commits. Keep it at the top of the description so squash-merge
preserves the mapping.

---

## 3. Code comments (upstream provenance)

Every file modified by an upstream port MUST include a comment referencing
the original commit:

### When adding new logic from upstream

```go
// Ported from upstream commit 61ee2d2e.
// Original: fix(db): use connection pool to avoid corrupted writes
db.SetMaxOpenConns(1)
```

Place the comment on the line immediately before the ported code.
If the port spans multiple lines, place it before the block.

### When modifying existing logic to match upstream

```go
// Updated from upstream commit e2e0bc09.
// Original: fix(config): scope .crush discovery to the current repo
// Change: stop walk at git working tree root
```

### When the port is a rename (CRUSH_ → LENOS_)

```go
// Adapted from upstream commit 6923820a.
// Original: feat(db): refuse to open a data directory in use by another crush
// Renamed: CRUSH_SKIP_DATADIR_LOCK → LENOS_SKIP_DATADIR_LOCK
```

---

## 4. Commit message format

```text
chore(upstream): port <upstream-hash> — <upstream-title>

Ported from upstream commit <hash>.
Original: <full commit subject>

<optional body describing adaptations>
```

### Examples

- `chore(upstream): port 61ee2d2e — fix(db): use connection pool to avoid corrupted writes`
- `chore(upstream): port 40108413 — fix(db): prevent SQLITE_NOTADB corruption under concurrent sub-agents`
- `chore(upstream): port 0585f498 — fix(pubsub): raise default per-subscriber buffer (64 → 4096)`

### When batching multiple upstream commits in one PR

Each commit in the PR gets its own `chore(upstream):` message:

```text
chore(upstream): port 6ed8852b — fix(agent): estimate missing streamed usage
chore(upstream): port 2e9c6505 — fix(agent): correct fallback usage accounting
chore(upstream): port 74e6e378 — fix(agent): harden fallback usage accounting
```

---

## 5. Keeping docs in sync

After merging a tracking PR:

1. Update the corresponding `v0.74-*.md` file to mark each ported commit
   as `✅ MERGED` or `❌ SKIPPED` with reasoning:

   ```markdown
   ### `61ee2d2e` — fix(db): use connection pool to avoid corrupted writes
   ✅ **MERGED** (PR #123)
   ```

2. When all applicable commits in a category file are resolved (all
   marked MERGED or SKIPPED), rename the file to `v0.74-*.done.md`.
   This makes it visually obvious which categories are complete vs.
   still pending, without needing to open each file.

   ```bash
   mv docs/upstream/v0.74-db.md docs/upstream/v0.74-db.done.md
   ```

3. Remove from the "Quick merge checklist" in README.md.

4. Keep the PR description in sync: if the PR evolves (e.g. a commit
   is reverted or skipped after initial porting), update the upstream
   references table in the PR body to reflect the final state.

---

## 6. Verification before PR merge

```bash
# 1. Run tests
go test ./internal/db/... ./internal/agent/... ./internal/pubsub/...

# 2. Check upstream provenance comments exist
grep -rn "Ported from upstream commit" internal/ | grep -v "_test.go"

# 3. Verify no upstream-id symbols leaked (CRUSH instead of LENOS)
grep -rn "CRUSH_" internal/ | grep -v "_test.go" | grep -v "docs/" || echo "clean"
```

---

## 7. Review checklist

When reviewing an upstream tracking PR, check:

- [ ] PR description has an upstream references table
- [ ] Each ported block has a `// Ported from upstream commit ...` comment
- [ ] No leaked `CRUSH_` prefixes (must be `LENOS_`)
- [ ] Tests pass
- [ ] Docs updated (v0.74-*.md, README.md)
