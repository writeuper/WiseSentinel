#!/usr/bin/env python3
"""Cleanup-safe live contract for authorized Agent Trace access.

Only outcome categories are printed: no API key, question, answer, trace ID,
or trace payload is stored in CI output.
"""

import argparse
import json
import sys
import uuid
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class Client:
    def __init__(self, base_url: str, api_key: str, timeout: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def request(self, method: str, path: str, payload: dict[str, Any] | None = None) -> tuple[int, dict[str, Any]]:
        body = json.dumps(payload, ensure_ascii=False).encode() if payload is not None else None
        req = Request(
            self.base_url + path,
            data=body,
            headers={"X-API-Key": self.api_key, "Content-Type": "application/json"},
            method=method,
        )
        try:
            with urlopen(req, timeout=self.timeout) as response:
                raw = response.read().decode()
                status = response.status
        except HTTPError as exc:
            raw = exc.read().decode(errors="ignore")
            status = exc.code
        except (URLError, TimeoutError) as exc:
            raise RuntimeError("network_error") from exc
        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError("invalid_json") from exc
        return status, envelope if isinstance(envelope, dict) else {}

    def success_data(self, method: str, path: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
        status, envelope = self.request(method, path, payload)
        if status != 200 or envelope.get("code") != 0 or not isinstance(envelope.get("data"), dict):
            raise RuntimeError("unexpected_api_result")
        return envelope["data"]


def run(client: Client) -> str:
    session_id = ""
    try:
        session_id = str(client.success_data("POST", "/sessions", {"title": "trace-access-contract", "agent_type": "chat"}).get("session_id") or "")
        if not session_id:
            return "missing_session_id"
        chat = client.success_data("POST", "/chat", {
            "session_id": session_id,
            "question": "请用一句话说明企业级 Agent 平台健康检查原则。",
            "options": {"enable_rag": False, "enable_tools": False},
        })
        trace_id = str(chat.get("trace_id") or "")
        if not trace_id:
            return "missing_trace_id"
        trace = client.success_data("GET", "/traces/" + trace_id)
        if not isinstance(trace.get("trace"), dict) or trace["trace"].get("trace_id") != trace_id:
            return "owned_trace_unreadable"
        status, missing = client.request("GET", "/traces/" + uuid.uuid4().hex)
        if status != 404 or missing.get("code") != 40401:
            return "missing_trace_not_fail_closed"
        return "trace_access_contract_success"
    finally:
        if session_id:
            status, envelope = client.request("DELETE", "/sessions/" + session_id)
            if status != 200 or envelope.get("code") != 0:
                raise RuntimeError("cleanup_failed")


def main() -> int:
    parser = argparse.ArgumentParser(description="Run authorized Trace access contract.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--timeout", type=int, default=120)
    args = parser.parse_args()
    try:
        outcome = run(Client(args.base_url, args.api_key, args.timeout))
    except RuntimeError as exc:
        outcome = str(exc)
    print(json.dumps({"outcome": outcome}, sort_keys=True))
    return 0 if outcome == "trace_access_contract_success" else 1


if __name__ == "__main__":
    raise SystemExit(main())
