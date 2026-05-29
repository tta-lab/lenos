# Message Blocks

Message blocks are the natural-language form for Lenos Bash. They let an
assistant response mix prose and shell while preserving the rule that one
assistant emit maps to one bash subprocess.

This document is an executable-facing spec for parser and runtime work. The
fixture corpus lives in `internal/agent/messageblock/testdata/fixtures.json`.

## Contract

Lenos Bash is bash plus top-level `m` message blocks.

Everything outside a message block is bash. Raw prose, markdown fences, and
tool wrappers are not valid response syntax.

A message block is valid only when `m` is the first non-whitespace token on its
own physical line. Leading indentation is allowed at top level. The block is
removed before the remaining bash is executed.

If bash remains after message removal, Lenos runs that bash once. If the bash
succeeds, Lenos publishes the extracted messages in source order. If the bash
fails, Lenos publishes none of the extracted messages and uses the normal
failure recovery flow.

If only message blocks remain, Lenos publishes them and ends the loop.

## Syntax

Basic form:

```bash
m"Done."
```

Multiline form:

```bash
m"First line.
Second line."
```

Raw multiline form:

```bash
m####"
Ready.

- First point.
- Second point.
"####
```

Raw-string form with hashes:

```bash
m#"Body with "quotes"."#
```

The `#` characters are raw-string delimiters, not bash comments. They follow
Rust raw string rules: the opener and closer must use the same number of
hashes, and the body is literal. Add enough hashes so the exact closing
delimiter does not appear inside the body.

Use more hashes when the body contains a delimiter candidate:

```bash
m##"Body with "# delimiter candidate."##
```

Addressed form:

```bash
m(neil)"Please review this."
```

When `--pair-with` is set, untargeted message blocks are also delivered to that
default target. Explicit `m(target)"..."` blocks override the default.

Mixed bash and message blocks:

```bash
m"Inspecting files."
rg "needle" .
```

Indented top-level message blocks are valid:

```bash
  m"Done."
```

## Non-Protocol Text

Message-looking text inside normal bash syntax is not speech.

Inside a heredoc body, it is literal heredoc content:

```bash
cat <<EOF
m"literal"
EOF
```

Inside a shell string, it is a string:

```bash
printf '%s\n' 'm"literal"'
```

Inside a comment, it is a comment:

```bash
# m"not speech"
echo ok
```

These forms extract no messages.

## Invalid Forms

Raw prose before bash is invalid:

```text
I will inspect files.
rg "needle" .
```

Fenced bash is invalid:

````markdown
```bash
ls
```
````

Same-line message forms are invalid:

```bash
echo ok; m"Done."
sleep 1 & m"Done."
printf ok | m"Done."
cat <<EOF m"Done."
EOF
```

Message blocks are invalid inside control flow, functions, substitutions, or
ordinary command words:

```bash
if true; then
  m"Done."
fi
```

```bash
for file in *; do
  m"Checking."
done
```

```bash
say_done() {
  m"Done."
}
```

```bash
value=$(m"Done.")
```

```bash
command m"Done."
```

Unterminated and mismatched raw blocks are invalid:

```bash
m"Started.
```

```bash
m##"body"#
```

Targets may contain only parser-approved agent-name characters. Whitespace is
invalid:

```bash
m(bad target)"Done."
```

Do not define convenience variants for old control-flow concepts. Message
blocks do not have a continue form; mixed bash plus `m` already continues
through normal bash result flow, and message-only `m` ends. Unknown words should
be handled by normal bash/runtime repair paths, not by adding named protocol
diagnostics.

## Fixture Shape

Each fixture records:

- `source`: original assistant emit.
- `messages`: extracted message events with target, body, and source position.
- `bash`: remaining bash source after message removal.
- `has_bash`: whether any executable bash remains.
- `lifecycle`: `message-only`, `continue`, or `parse-error`.
- `error`: expected parse diagnostic, when the source is invalid.

The fixture corpus is intentionally parser-facing. Runtime work may add more
tests for execution and storage, but it should preserve these examples as the
shared protocol baseline.
