# TASKS4: Update System Prompt For Lenos Bash Message Blocks

## Goal

Update the model-facing system prompt after runtime support exists. The prompt
should teach the surface contract only, not parser internals.

## Dependency

Start after TASKS2 and TASKS2.1. Prefer starting after TASKS3 if prefill
wording needs to align with provider behavior.

## References

- `flicknote://note/09b84178-47fe-49dc-bb40-a3ab62c2bd84`
- `flicknote://note/74688c39-dde4-4333-92d2-5330f99e0f22`
- `flicknote://note/ca4e2e09-f70d-4cfe-a9d2-c066df03a9a3`
- `flicknote://note/202ae86e-ed37-4497-85c2-299618cb6403`

## Prompt Rules

Teach:

- response is Lenos Bash;
- everything outside message blocks is bash;
- natural language must use `m`;
- `m` must be the first non-whitespace token on its own physical line;
- short private work notes use `# comment`;
- message-only `m` ends;
- mixed bash plus message blocks follows normal bash loop behavior;
- message blocks publish only after bash succeeds;
- do not write `echo ok; m"done"` or put `m` after `&`, `|`, or heredoc setup;
- `m"..."` inside heredoc content is just file/stdin content, not speech;
- raw markdown/prose/fences/tool wrappers are invalid.

Do not teach:

- `mvdan`;
- AST details;
- source-span removal;
- storage internals;
- old `:md` history;
- `narrate`, unless retained as an advanced/internal fallback outside the main
  prompt.

## Tests

- Update system prompt golden tests.
- Add examples that exact-match valid raw responses:
  - `m"Done."`;
  - `m"Inspecting files."\n\nrg "needle" .`;
  - `m(neil)#"Please review "message block" parsing."#`;
  - pure bash;
  - `# comment` plus bash.
- Add invalid examples only if the prompt has a concise "do not" section:
  - `echo ok; m"Done."` is invalid because `m` is not on its own line;
  - `cat <<EOF\nm"literal"\nEOF` writes literal heredoc content and does not
    speak.
- Ensure old `:md` examples are gone.
- Ensure prompt does not describe implementation internals.

## Acceptance

- Prompt is shorter than the current narrate-heavy explanation if possible.
- Prompt uses the phrase `speak natural language` so the model maps prose,
  status, plans, greetings, and final answers to message blocks.
- Prompt keeps pure bash as a first-class valid response.
- Golden tests pass.
