# Salvage

Salvage handles one common mixed response:

```text
I'll inspect the repo.
cat README.md && ls
```

When safe, the runtime rewrites it to:

```bash
# I'll inspect the repo.
cat README.md && ls
```

This keeps useful shell work from being lost while still avoiding broad,
guessy prose-to-bash rewrites.

## Gate

Salvage runs before the general natural-language rewrite. It only fires when:

- The first line is natural language.
- There is non-empty content after the first line.
- The rest is not a bare exit-like marker.
- The rest has valid bash syntax according to `bash -n`.
- The first effective command in the rest has shell-action evidence.

Shell-action evidence includes assignment prefixes, `&&`, `||`, pipes,
redirects, semicolons, flags, path-like tokens, or an executable path.

For command words, the runtime probes command existence through the same
runner abstraction used for bash. For path commands such as `./scripts/test.sh`
or `/usr/local/bin/tool`, it probes executability in the sandbox.

## Non-Goals

Salvage is not a general bash classifier. It is a narrow repair for
"first line prose, rest looks like shell." If the rest is ambiguous prose such
as `go ahead`, salvage does not fire; the full emit becomes narration.

## Storage

When salvage fires, the rewritten bash is stored in the assistant message and
in the command result. The original prose line becomes a bash comment.
