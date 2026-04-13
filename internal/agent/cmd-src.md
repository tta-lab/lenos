Browse, read, and edit source code with symbol awareness

src understands the structure of source files — functions, types, methods — and lets you navigate and modify them precisely without guessing line numbers.

## Reading

Browse file structure:
  src <file>                  # symbol tree (depth 2)
  src <file> --depth 3       # deeper tree
  src <file> -s <id>          # read symbol by ID
  src <file> --tree           # force tree view

Read a specific symbol:
  src <file> -s <id>          # 2-char symbol ID from tree output
  src <file> -s 3f            # read symbol by ID

Read markdown files:
  src README.md               # heading-based sections with IDs
  src README.md -s 3K          # read section by 2-char ID
  src README.md --tree         # show heading tree

## Editing

**Each `src edit` call performs exactly one text replacement.** The edit block uses `===BEFORE===` and `===AFTER===` as delimiters — each block must contain exactly one of each. To make multiple changes, issue separate `src edit` calls.

Targeted replacement within one symbol:
  src edit <file> --section <id> <<'EOF'
  ===BEFORE===
  old text
  ===AFTER===
  new text
  EOF

Global text replacement (any text anywhere in file):
  src edit <file> <<'EOF'
  ===BEFORE===
  old text
  ===AFTER===
  new text
  EOF

**Single-edit example — the complete correct form:**
  src edit some/file.go <<'EOF'
  ===BEFORE===
  func greet() {
      fmt.Println("hello")
  }
  ===AFTER===
  func greet() {
      fmt.Println("hello, world")
  }
  EOF

- `===BEFORE===` marks the start of the text you want to match (the existing text in the file)
- `===AFTER===` marks the start of the replacement text
- If you need two edits, call `src edit` twice — never put two `===BEFORE===`/`===AFTER===` pairs in one call

Replace entire symbol by ID (stdin-based):
  echo "func newImpl() {}" | src replace <file> -s <id>

Insert before/after a symbol:
  echo "// new" | src insert <file> --after <id>
  echo "// new" | src insert <file> --before <id>

Delete a symbol or dead code:
  src delete <file> -s <id>
## src edit Matching

`src edit` tries 4 matching passes in order — you usually don't need exact whitespace:
1. exact byte match
2. trim trailing spaces/tabs
3. trim both ends + auto-reindent
4. unicode-fold (curly quotes → ASCII)

If a non-exact pass fires, src prints the adjustment and your edit proceeds.

**Multi-match**: if the same text appears in multiple places, src errors with line numbers. Fix: use `--section <id>` or add more context.

## If src edit fails

src edit shows the closest region it found and which pass failed.

Recovery steps:
- **text not found**: Add more surrounding lines to your ===BEFORE=== block, or use `--section <id>` for symbol-level targeting
- **found N matches**: Use `--section <id>` to disambiguate, or add 3+ context lines
- **pass failed**: Re-scan with `src <file>` to get fresh IDs

**If none of these work**: run `ttal alert "src failed: <reason>"` — do not use sed, awk, perl, or python to edit files.

## ⚠️ STOP — File Editing Rules

**Only two tools may modify files:**

1. **`src edit`** — symbol-aware editing (preferred for targeted changes)
2. **`heredoc redirection`** — `cat <<"EOF" > file` for whole-file writes

**NEVER use these tools to modify files:** `sed -i`, `perl -i`, `awk ... > file`, `python script.py` (when writing), `printf ... > file`.

You may freely use `sed`, `perl`, `awk`, `python` to **read** or **transform** data in pipelines — the restriction is only on writing changes to disk.
