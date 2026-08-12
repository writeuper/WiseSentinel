#!/usr/bin/env python3
"""Validate Compose dependency and restart invariants for the RAG stack."""

from __future__ import annotations

import json
import subprocess
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
COMPOSE = ROOT / "manifest" / "docker" / "docker-compose.yml"


def compose_config() -> dict:
    result = subprocess.run(
        ["docker", "compose", "-f", str(COMPOSE), "config", "--format", "json"],
        cwd=ROOT,
        check=True,
        capture_output=True,
        text=True,
    )
    return json.loads(result.stdout)


class ComposeResilienceTest(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.services = compose_config()["services"]

    def test_milvus_stack_restarts_after_process_failure(self) -> None:
        for name in ("etcd", "minio", "milvus"):
            self.assertEqual(
                self.services[name].get("restart"),
                "unless-stopped",
                f"{name} must restart after an unplanned dependency/process exit",
            )

    def test_milvus_waits_for_healthy_dependencies(self) -> None:
        dependencies = self.services["milvus"]["depends_on"]
        self.assertEqual(dependencies["etcd"]["condition"], "service_healthy")
        self.assertEqual(dependencies["minio"]["condition"], "service_healthy")

    def test_consumers_wait_for_healthy_milvus(self) -> None:
        for name in ("platform", "attu"):
            dependency = self.services[name]["depends_on"]["milvus"]
            self.assertEqual(
                dependency["condition"],
                "service_healthy",
                f"{name} must not start against an unhealthy Milvus",
            )


if __name__ == "__main__":
    unittest.main()
