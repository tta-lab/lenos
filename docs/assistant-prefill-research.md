# Assistant Prefill Research

Date: 2026-05-07

## Question

Lenos is a bash-first agent runtime: every assistant emit is passed to
`bash -c`. Some models often start with invalid shapes such as prose
(`Let's do it`), markdown fences such as ```` ```bash ````, or tool-call
wrappers (`<tool_call>`). We investigated whether assistant prefill can bias
models to start with a bash comment (`# `), so prose becomes valid bash and the
next line is more likely to be an executable command.

## Key Finding

DeepSeek's chat prefix API works for this use case.

When we sent the final assistant message as:

```json
{
  "role": "assistant",
  "content": "# ",
  "prefix": true
}
```

DeepSeek returned only the continuation. With the short Lenos-like prompt, it
returned:

```text
0.1. Check the current working directory and list files
pwd && ls -la
exit 0
```

The actual bash bytes are `prefill + content`, so the executable text is:

```bash
# 0.1. Check the current working directory and list files
pwd && ls -la
exit 0
```

With the real `lenos system-prompt`, it returned:

```text
检查当前目录内容
pwd && ls -la && exit
```

The actual bash is:

```bash
# 检查当前目录内容
pwd && ls -la && exit
```

This is exactly the behavior we want: the model's note becomes a shell comment,
then it emits a real bash command.

## Provider Matrix

| Provider | Result | API shape | Notes |
|---|---|---|---|
| DeepSeek | Works, tested | Last assistant message has `prefix: true`; use beta base URL | Returns continuation-only. Tested with `deepseek-v4-flash`. |
| Mistral | Documented support, not tested here | Last assistant message has `prefix: true` | Official docs say response starts with the exact prefix, then continues. Need test return shape before integration. |
| Groq | Documented support, not tested here | Last assistant message contains prefill content | Official docs call this Assistant Message Prefilling. |
| Anthropic Claude | Partially supported | Last assistant message is treated as prefill | Newer Claude Mythos Preview, Opus 4.7, Opus 4.6, and Sonnet 4.6 do not support it and return 400. Older/current supported models such as Sonnet 4.5 do. |
| Xiaomi MiMo native | Does not work, tested | OpenAI-compatible final assistant message | `mimo-v2.5` treated the partial assistant message as prior context, used reasoning tokens, and did not continue the prefix. |
| OpenAI Responses API | Does not work for this, tested | Assistant input item with `# ` | Returned `ls\nexit` rather than continuing from `# `. Valid bash, but no useful prefill behavior. |
| OpenAI Chat Completions / Codex | No documented support found | N/A | `prediction` is not equivalent to assistant prefill. |
| OpenRouter | Unclear / not generic | Varies by routed provider | OpenRouter docs do not document generic prefill. Model metadata did not expose a `prefix` supported parameter. Treat per-route only. |
| LiteLLM | Adapter support | Normalizes DeepSeek / Mistral / Anthropic | Useful reference for provider capability metadata. |

## Test Commands

The local probe lives in `scripts/check_prefill.py`.

DeepSeek JSON continuation proof:

```bash
python3 scripts/check_prefill.py --provider deepseek --print-response
```

Expected behavior:

```text
prefill: '{"prefill_marker":"ZXQ73","continued":'
content: 'true}'
```

Full answer after concatenation:

```json
{"prefill_marker":"ZXQ73","continued":true}
```

DeepSeek bash-first prompt:

```bash
python3 scripts/check_prefill.py \
  --provider deepseek \
  --lenos-bash \
  --max-tokens 128 \
  --print-response
```

DeepSeek with real Lenos prompt:

```bash
python3 scripts/check_prefill.py \
  --provider deepseek \
  --lenos-bash \
  --use-lenos-system-prompt \
  --lenos-system-prompt-command "lenos system-prompt" \
  --max-tokens 256 \
  --print-response
```

MiMo native negative test:

```bash
python3 scripts/check_prefill.py --provider mimo --print-response
```

OpenAI Responses negative test:

```bash
python3 scripts/check_prefill.py \
  --provider openai-responses \
  --lenos-bash \
  --model gpt-5.3-codex \
  --max-tokens 128 \
  --print-response
```

## Design Implication for Lenos

For providers that support real assistant prefill, use a minimal `# ` assistant
prefix before each bash-first generation. This is a soft shape guard:

```bash
# 
```

It mainly fixes the common "note before action" failure mode. If the model
starts with prose, that prose lands on a bash comment line. It does not fully
solve markdown fences or tool-call wrappers, so the runtime gates must remain:

- Reject XML/JSON/bracket tool-call wrapper emits.
- Reject markdown fence emits.
- Reject un-commented prose prefixes.
- Keep `bash -n` syntax checks before execution.

Do not auto-extract commands from malformed tool-call wrappers. That would
teach the model that invalid shapes still execute and would make auditability
worse.

## Recommended Integration Rule

Add provider capability metadata, not a global behavior:

```text
assistant_prefill:
  mode: none | final_assistant | assistant_prefix_flag
  prefix_text: "# "
```

Suggested initial mapping:

```text
deepseek: assistant_prefix_flag
mistral: assistant_prefix_flag, pending local test
groq: final_assistant, pending local test
anthropic: final_assistant for supported older models only
mimo: none
openai: none
openrouter: none by default; only enable for known routed providers after test
```

## Sources

- DeepSeek Chat Prefix Completion: https://api-docs.deepseek.com/guides/chat_prefix_completion
- DeepSeek Chat Completion API: https://api-docs.deepseek.com/api/create-chat-completion
- Mistral Chat Completion prefix docs: https://docs.mistral.ai/studio-api/conversations/chat-completion
- Mistral prefix guide: https://docs.mistral.ai/guides/prefix
- Groq Assistant Message Prefilling: https://console.groq.com/docs/prefilling
- Anthropic Messages examples: https://docs.anthropic.com/en/docs/build-with-claude/working-with-messages
- LiteLLM prefix support: https://docs.litellm.ai/docs/completion/prefix
