# Classifier

`internal/agent/classify.go` decides what the runtime does with one model
emit before execution.

## Order

1. Empty text: re-prompt.
2. Bare `exit` or `exit N`: end the loop.
3. Blocked edit patterns such as `sed -i` and `perl -i`: re-prompt.
4. Tool-call-shaped wrappers: delete the bad assistant row and re-prompt.
5. Prose-first-line salvage: rewrite to commented bash when the rest is
   strongly bash-shaped.
6. Natural-language emit: rewrite to `narrate` heredoc.
7. `bash -n` syntax check: re-prompt on syntax failure.
8. Everything else: execute as bash.

There is no markdown protocol branch.

## Natural-Language Detector

An emit is natural language when the first non-whitespace byte satisfies:

- It is not a lowercase English letter.
- It is not `#`.
- If it is an uppercase English letter, the first line does not contain `=`.

Markdown-style headings starting with more than one `#` are also natural
language. This catches `## Done` and `### Done` as prose, while allowing a
single `#` bash comment.

CJK prose works because the first byte is neither lowercase ASCII nor `#`.

## Why The Detector Is Simple

The classifier is not trying to fully understand English or shell. It only
separates high-confidence prose from normal bash-shaped output. Ambiguous
lowercase strings still run as bash and recover through command-not-found
guidance when needed.

## Old Markers

Strings like `:md`, `:continue`, and `:exit` no longer have protocol meaning.
If they appear in model output, they are just text inside the bash-only
classification flow.
