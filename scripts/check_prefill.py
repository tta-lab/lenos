#!/usr/bin/env python3
"""Try assistant prefill behavior against OpenAI-compatible chat APIs."""

from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from dataclasses import dataclass
from pathlib import Path
from typing import Any


DEFAULT_PROMPT = (
    "Complete the partial assistant response by closing the JSON. "
    "Do not start a new object. Do not add markdown."
)
DEFAULT_PREFILL = '{"prefill_marker":"ZXQ73","continued":'
LENOS_BASH_PROMPT = (
    "You are in a bash-first runtime. Emit one harmless bash command that "
    "inspects the current directory, then emit exit if the task is complete."
)
SHORT_LENOS_SYSTEM_PROMPT = """You are running inside a bash-first runtime.
Every response is executed as bash. There is no chat channel and no markdown renderer.
Valid shapes are raw bash commands, bash comments starting with #, m message blocks for prose, and exit.
Do not emit markdown fences, XML/JSON tool calls, or plain English at top level.
If you want to leave a note before a command, write it as a bash comment:
# short note
then write the command on the next line."""


@dataclass(frozen=True)
class Provider:
    name: str
    api_kind: str
    api_key_env: str
    model_env: str
    base_url_env: str
    default_model: str
    default_base_url: str
    default_prefix_flag: bool
    include_max_tokens: bool = True


PROVIDERS = {
    "deepseek": Provider(
        name="deepseek",
        api_kind="chat",
        api_key_env="DEEPSEEK_API_KEY",
        model_env="DEEPSEEK_MODEL",
        base_url_env="DEEPSEEK_BASE_URL",
        default_model="deepseek-v4-flash",
        default_base_url="https://api.deepseek.com/beta",
        default_prefix_flag=True,
    ),
    "mimo": Provider(
        name="mimo",
        api_kind="chat",
        api_key_env="MIMO_API_KEY",
        model_env="MIMO_MODEL",
        base_url_env="MIMO_BASE_URL",
        default_model="mimo-v2.5",
        default_base_url="https://token-plan-sgp.xiaomimimo.com/v1",
        default_prefix_flag=False,
    ),
    "openrouter-mimo": Provider(
        name="openrouter-mimo",
        api_kind="chat",
        api_key_env="OPENROUTER_API_KEY",
        model_env="OPENROUTER_MIMO_MODEL",
        base_url_env="OPENROUTER_BASE_URL",
        default_model="xiaomi/mimo-v2.5",
        default_base_url="https://openrouter.ai/api/v1",
        default_prefix_flag=False,
    ),
    "openai-chat": Provider(
        name="openai-chat",
        api_kind="chat",
        api_key_env="OPENAI_API_KEY",
        model_env="OPENAI_MODEL",
        base_url_env="OPENAI_BASE_URL",
        default_model="gpt-5.3-codex",
        default_base_url="https://api.openai.com/v1",
        default_prefix_flag=False,
    ),
    "openai-responses": Provider(
        name="openai-responses",
        api_kind="responses",
        api_key_env="OPENAI_API_KEY",
        model_env="OPENAI_MODEL",
        base_url_env="OPENAI_BASE_URL",
        default_model="gpt-5.3-codex",
        default_base_url="https://api.openai.com/v1",
        default_prefix_flag=False,
    ),
    "codex": Provider(
        name="codex",
        api_kind="responses",
        api_key_env="CODEX_API_KEY",
        model_env="CODEX_MODEL",
        base_url_env="CODEX_BASE_URL",
        default_model="gpt-5.3-codex",
        default_base_url="https://chatgpt.com/backend-api/codex",
        default_prefix_flag=False,
        include_max_tokens=False,
    ),
}


def load_env_file(path: Path) -> None:
    for line_number, raw_line in enumerate(path.read_text().splitlines(), 1):
        line = raw_line.strip()
        if not line or line.startswith("#"):
            continue
        if line.startswith("export "):
            line = line[len("export ") :].strip()
        if "=" not in line:
            raise ValueError(f"{path}:{line_number}: expected KEY=VALUE")
        key, value = line.split("=", 1)
        key = key.strip()
        value = value.strip().strip("'\"")
        if key:
            os.environ.setdefault(key, value)


def build_payload(
    model: str,
    prompt: str,
    prefill: str,
    max_tokens: int,
    temperature: float,
    prefix_flag: bool,
    system_prompt: str | None,
) -> dict[str, Any]:
    assistant: dict[str, Any] = {
        "role": "assistant",
        "content": prefill,
    }
    if prefix_flag:
        assistant["prefix"] = True

    messages: list[dict[str, Any]] = []
    if system_prompt:
        messages.append(
            {
                "role": "system",
                "content": system_prompt,
            }
        )
    messages.extend(
        [
            {
                "role": "user",
                "content": prompt,
            },
            assistant,
        ]
    )

    return {
        "model": model,
        "messages": messages,
        "max_completion_tokens": max_tokens,
        "temperature": temperature,
        "stream": False,
    }


def build_responses_payload(
    model: str,
    prompt: str,
    prefill: str,
    max_tokens: int,
    include_max_tokens: bool,
    system_prompt: str | None,
) -> dict[str, Any]:
    payload: dict[str, Any] = {
        "model": model,
        "input": [
            {
                "role": "user",
                "content": prompt,
            },
            {
                "role": "assistant",
                "type": "message",
                "phase": "final_answer",
                "content": prefill,
            },
        ],
    }
    if include_max_tokens:
        payload["max_output_tokens"] = max_tokens
    if system_prompt:
        payload["instructions"] = system_prompt
    return payload


def resolve_prompt_defaults(
    lenos_bash: bool,
    prompt: str | None,
    prefill: str | None,
    system_prompt: str | None,
) -> tuple[str, str, str | None]:
    if not lenos_bash:
        return prompt or DEFAULT_PROMPT, prefill or DEFAULT_PREFILL, system_prompt
    return (
        prompt or LENOS_BASH_PROMPT,
        prefill if prefill is not None else "# ",
        system_prompt or SHORT_LENOS_SYSTEM_PROMPT,
    )


def read_lenos_system_prompt(command: str) -> str:
    completed = subprocess.run(
        command,
        shell=True,
        check=True,
        text=True,
        encoding="utf-8",
        errors="replace",
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
    )
    return completed.stdout


def chat_completions_url(base_url: str) -> str:
    return base_url.rstrip("/") + "/chat/completions"


def responses_url(base_url: str) -> str:
    return base_url.rstrip("/") + "/responses"


def request_headers(provider: Provider, api_key: str) -> dict[str, str]:
    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_key}",
    }
    if provider.name == "mimo":
        headers["api-key"] = api_key
    if provider.name == "openrouter-mimo":
        headers["HTTP-Referer"] = "https://github.com/tta-lab/lenos"
        headers["X-Title"] = "lenos-prefill-check"
    if provider.name == "codex":
        account_id = os.environ.get("CODEX_ACCOUNT_ID")
        if account_id:
            headers["ChatGPT-Account-Id"] = account_id
        headers["originator"] = os.environ.get("CODEX_ORIGINATOR", "codex_cli_rs")
        headers["OpenAI-Beta"] = os.environ.get("CODEX_OPENAI_BETA", "responses=experimental")
    headers.update(extra_headers_from_env(provider))
    return headers


def extra_headers_from_env(provider: Provider) -> dict[str, str]:
    headers: dict[str, str] = {}
    env_names = [
        "EXTRA_HEADERS_JSON",
        f"{provider.name.upper().replace('-', '_')}_EXTRA_HEADERS_JSON",
    ]
    for env_name in env_names:
        raw = os.environ.get(env_name)
        if not raw:
            continue
        parsed = json.loads(raw)
        if not isinstance(parsed, dict):
            raise ValueError(f"{env_name} must be a JSON object")
        for key, value in parsed.items():
            headers[str(key)] = str(value)
    return headers


def post_json(url: str, headers: dict[str, str], payload: dict[str, Any]) -> dict[str, Any]:
    request = urllib.request.Request(
        url,
        data=json.dumps(payload).encode("utf-8"),
        headers=headers,
        method="POST",
    )
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as error:
        body = error.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"HTTP {error.code} from {url}:\n{body}") from error


def extract_content(response: dict[str, Any]) -> str:
    if isinstance(response.get("output_text"), str):
        return response["output_text"]

    output = response.get("output")
    if isinstance(output, list):
        chunks: list[str] = []
        for item in output:
            if not isinstance(item, dict):
                continue
            for part in item.get("content") or []:
                if isinstance(part, dict):
                    text = part.get("text")
                    if isinstance(text, str):
                        chunks.append(text)
        if chunks:
            return "".join(chunks)

    choices = response.get("choices") or []
    if not choices:
        return ""
    message = choices[0].get("message") or {}
    return message.get("content") or ""


def summarize(prefill: str, content: str) -> str:
    stripped = content.lstrip()
    if stripped.startswith(prefill):
        return "looks like full prefilled text was returned"
    if stripped.startswith("true") or stripped.startswith(" true"):
        return "looks like continuation-only text was returned"
    if "prefill_marker" in stripped or "ZXQ73" in stripped:
        return "marker appeared, but shape needs manual inspection"
    return "no obvious prefill marker in returned content"


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Send a final assistant message to test prefill behavior.",
    )
    parser.add_argument(
        "--provider",
        choices=sorted(PROVIDERS),
        default="mimo",
        help="Endpoint preset to use.",
    )
    parser.add_argument("--env-file", type=Path, help="Optional .env file to load.")
    parser.add_argument("--api-key", help="API key. Defaults to provider env var.")
    parser.add_argument("--model", help="Model. Defaults to provider env var or preset.")
    parser.add_argument("--base-url", help="Base URL. Defaults to provider env var or preset.")
    parser.add_argument("--prompt")
    parser.add_argument("--prefill")
    parser.add_argument("--system-prompt")
    parser.add_argument("--system-prompt-file", type=Path)
    parser.add_argument(
        "--lenos-bash",
        action="store_true",
        help="Use a short lenos-like bash-first system prompt and '# ' prefill.",
    )
    parser.add_argument(
        "--use-lenos-system-prompt",
        action="store_true",
        help="Shell out and use the real lenos system prompt as the system prompt.",
    )
    parser.add_argument(
        "--lenos-system-prompt-command",
        default="go run . system-prompt",
        help="Command used by --use-lenos-system-prompt.",
    )
    parser.add_argument("--max-tokens", type=int, default=24)
    parser.add_argument("--temperature", type=float, default=0)
    parser.add_argument(
        "--prefix-flag",
        action="store_true",
        help="Add DeepSeek-style prefix:true to the assistant message.",
    )
    parser.add_argument(
        "--no-prefix-flag",
        action="store_true",
        help="Do not add prefix:true, even for providers whose preset enables it.",
    )
    parser.add_argument("--print-request", action="store_true")
    parser.add_argument("--print-response", action="store_true")
    return parser.parse_args(argv)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    if args.env_file:
        load_env_file(args.env_file)

    provider = PROVIDERS[args.provider]
    api_key = args.api_key or os.environ.get(provider.api_key_env)
    if not api_key:
        print(f"Missing API key. Set {provider.api_key_env} or pass --api-key.", file=sys.stderr)
        return 2

    model = args.model or os.environ.get(provider.model_env) or provider.default_model
    base_url = args.base_url or os.environ.get(provider.base_url_env) or provider.default_base_url
    system_prompt = args.system_prompt
    if args.system_prompt_file:
        system_prompt = args.system_prompt_file.read_text()
    if args.use_lenos_system_prompt:
        system_prompt = read_lenos_system_prompt(args.lenos_system_prompt_command)
    prompt, prefill, system_prompt = resolve_prompt_defaults(
        lenos_bash=args.lenos_bash,
        prompt=args.prompt,
        prefill=args.prefill,
        system_prompt=system_prompt,
    )
    prefix_flag = provider.default_prefix_flag
    if args.prefix_flag:
        prefix_flag = True
    if args.no_prefix_flag:
        prefix_flag = False

    if provider.api_kind == "responses":
        payload = build_responses_payload(
            model=model,
            prompt=prompt,
            prefill=prefill,
            max_tokens=args.max_tokens,
            include_max_tokens=provider.include_max_tokens,
            system_prompt=system_prompt,
        )
    else:
        payload = build_payload(
            model=model,
            prompt=prompt,
            prefill=prefill,
            max_tokens=args.max_tokens,
            temperature=args.temperature,
            prefix_flag=prefix_flag,
            system_prompt=system_prompt,
        )

    if args.print_request:
        printable = dict(payload)
        print(json.dumps(printable, indent=2, ensure_ascii=False))

    url = responses_url(base_url) if provider.api_kind == "responses" else chat_completions_url(base_url)
    response = post_json(url, request_headers(provider, api_key), payload)
    content = extract_content(response)

    print(f"provider: {provider.name}")
    print(f"api_kind: {provider.api_kind}")
    print(f"model: {model}")
    print(f"url: {url}")
    print(f"prefix_flag: {prefix_flag}")
    print(f"prefill: {prefill!r}")
    print(f"content: {content!r}")
    print(f"heuristic: {summarize(prefill, content)}")

    if args.print_response:
        print(json.dumps(response, indent=2, ensure_ascii=False))

    return 0


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
