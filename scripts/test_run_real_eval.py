#!/usr/bin/env python3
"""Regression checks for real-provider evaluation preflight guards."""

import os
import subprocess
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "scripts" / "run_real_eval.sh"
PROVIDER_VARS = ("LLM_API_KEY", "LLM_BASE_URL", "LLM_MODEL", "EMBED_API_KEY")


class RealEvalPreflightTests(unittest.TestCase):
    def run_script(self, overrides: dict[str, str]) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        for name in PROVIDER_VARS:
            env.pop(name, None)
        env.update(overrides)
        return subprocess.run(["bash", str(SCRIPT)], cwd=ROOT, env=env, text=True, capture_output=True, timeout=10)

    def test_rejects_missing_provider_credentials_before_deployment(self) -> None:
        result = self.run_script({})
        self.assertEqual(result.returncode, 2)
        self.assertIn("real evaluation prerequisite missing: LLM_API_KEY", result.stderr)

    def test_rejects_unverified_gold_before_deployment(self) -> None:
        result = self.run_script({
            "LLM_API_KEY": "test-key",
            "LLM_BASE_URL": "http://127.0.0.1:1",
            "LLM_MODEL": "test-model",
            "EMBED_API_KEY": "test-embedding-key",
        })
        self.assertEqual(result.returncode, 2)
        self.assertIn("strict relevance gate requires at least 100 verified samples", result.stderr)


if __name__ == "__main__":
    unittest.main()
