#!/usr/bin/env python3

from __future__ import annotations

import importlib.util
import os
import sys
import tempfile
import unittest
from pathlib import Path


SCRIPT = Path(__file__).with_name("check_prefill.py")
SPEC = importlib.util.spec_from_file_location("check_prefill", SCRIPT)
check_prefill = importlib.util.module_from_spec(SPEC)
assert SPEC and SPEC.loader
sys.modules["check_prefill"] = check_prefill
SPEC.loader.exec_module(check_prefill)


class CheckPrefillTest(unittest.TestCase):
    def test_default_models_for_primary_trials(self) -> None:
        self.assertEqual(
            check_prefill.PROVIDERS["deepseek"].default_model,
            "deepseek-v4-flash",
        )
        self.assertEqual(
            check_prefill.PROVIDERS["mimo"].default_model,
            "mimo-v2.5",
        )
        self.assertEqual(
            check_prefill.PROVIDERS["mimo"].default_base_url,
            "https://token-plan-sgp.xiaomimimo.com/v1",
        )

    def test_deepseek_payload_sets_prefix_true(self) -> None:
        payload = check_prefill.build_payload(
            model="deepseek-v4-flash",
            prompt="finish",
            prefill='{"x":',
            max_tokens=8,
            temperature=0,
            prefix_flag=True,
            system_prompt=None,
        )

        assistant = payload["messages"][-1]
        self.assertEqual(assistant["role"], "assistant")
        self.assertEqual(assistant["content"], '{"x":')
        self.assertIs(assistant["prefix"], True)

    def test_mimo_payload_omits_prefix_flag_by_default(self) -> None:
        payload = check_prefill.build_payload(
            model="mimo-v2.5-pro",
            prompt="finish",
            prefill='{"x":',
            max_tokens=8,
            temperature=0,
            prefix_flag=False,
            system_prompt=None,
        )

        self.assertNotIn("prefix", payload["messages"][-1])

    def test_chat_payload_can_include_system_prompt(self) -> None:
        payload = check_prefill.build_payload(
            model="deepseek-v4-flash",
            prompt="emit bash",
            prefill="# ",
            max_tokens=32,
            temperature=0,
            prefix_flag=True,
            system_prompt="Every response is bash.",
        )

        self.assertEqual(payload["messages"][0]["role"], "system")
        self.assertEqual(payload["messages"][0]["content"], "Every response is bash.")
        self.assertEqual(payload["messages"][-1]["content"], "# ")

    def test_lenos_bash_defaults_use_comment_prefill_and_short_prompt(self) -> None:
        prompt, prefill, system_prompt = check_prefill.resolve_prompt_defaults(
            lenos_bash=True,
            prompt=None,
            prefill=None,
            system_prompt=None,
        )

        self.assertIn("bash-first runtime", prompt)
        self.assertEqual(prefill, "# ")
        self.assertIn("Every response is executed as bash", system_prompt)

    def test_responses_payload_uses_assistant_message_input(self) -> None:
        payload = check_prefill.build_responses_payload(
            model="gpt-5.3-codex",
            prompt="finish",
            prefill='{"x":',
            max_tokens=8,
            include_max_tokens=True,
            system_prompt=None,
        )

        self.assertEqual(payload["input"][-1]["role"], "assistant")
        self.assertEqual(payload["input"][-1]["phase"], "final_answer")
        self.assertEqual(payload["input"][-1]["content"], '{"x":')
        self.assertEqual(payload["max_output_tokens"], 8)

    def test_env_file_loader_accepts_export_and_quotes(self) -> None:
        with tempfile.TemporaryDirectory() as temp_dir:
            path = Path(temp_dir) / ".env"
            path.write_text("export TEST_PREFILL_KEY='abc123'\n")
            old = os.environ.pop("TEST_PREFILL_KEY", None)
            try:
                check_prefill.load_env_file(path)
                self.assertEqual(os.environ["TEST_PREFILL_KEY"], "abc123")
            finally:
                os.environ.pop("TEST_PREFILL_KEY", None)
                if old is not None:
                    os.environ["TEST_PREFILL_KEY"] = old

    def test_codex_headers_include_optional_account_id(self) -> None:
        old_key = os.environ.get("CODEX_ACCOUNT_ID")
        os.environ["CODEX_ACCOUNT_ID"] = "acct_123"
        try:
            headers = check_prefill.request_headers(
                check_prefill.PROVIDERS["codex"],
                "token",
            )
        finally:
            if old_key is None:
                os.environ.pop("CODEX_ACCOUNT_ID", None)
            else:
                os.environ["CODEX_ACCOUNT_ID"] = old_key

        self.assertEqual(headers["Authorization"], "Bearer token")
        self.assertEqual(headers["ChatGPT-Account-Id"], "acct_123")
        self.assertEqual(headers["OpenAI-Beta"], "responses=experimental")


if __name__ == "__main__":
    unittest.main()
