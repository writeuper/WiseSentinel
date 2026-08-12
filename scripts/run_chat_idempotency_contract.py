#!/usr/bin/env python3
"""Cleanup-safe live contract for synchronous Chat idempotency.

Only aggregate states and response identities are printed. API keys, questions,
answers and raw HTTP bodies are never emitted.
"""

import argparse
import concurrent.futures
import hashlib
import json
import sys
import threading
import uuid
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEFAULT_QUESTION = "请用一句话说明企业级 Agent 平台健康检查的原则。"
CONFLICT_CODE = 40901


class Client:
    def __init__(self, base_url: str, api_key: str, tenant_id: str, timeout: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.tenant_id = tenant_id
        self.timeout = timeout

    def request(self, method: str, path: str, payload: dict[str, Any] | None = None, extra_headers: dict[str, str] | None = None) -> dict[str, Any]:
        headers = {"X-API-Key": self.api_key, "X-Tenant-ID": self.tenant_id, "Content-Type": "application/json"}
        if extra_headers:
            headers.update(extra_headers)
        data = json.dumps(payload, ensure_ascii=False).encode() if payload is not None else None
        request = Request(self.base_url + path, data=data, headers=headers, method=method)
        try:
            with urlopen(request, timeout=self.timeout) as response:
                raw = response.read().decode()
        except HTTPError as exc:
            raw = exc.read().decode(errors="ignore")
            try:
                code = json.loads(raw).get("code")
            except (json.JSONDecodeError, AttributeError):
                code = None
            raise RuntimeError(f"api_{code}" if code else f"http_{exc.code}") from exc
        except URLError as exc:
            raise RuntimeError("network_error") from exc
        envelope = json.loads(raw)
        if not isinstance(envelope, dict) or envelope.get("code") != 0:
            raise RuntimeError(f"api_{envelope.get('code') if isinstance(envelope, dict) else 'invalid'}")
        return envelope.get("data") if isinstance(envelope.get("data"), dict) else {}

    def create_session(self) -> str:
        session_id = self.request("POST", "/sessions", {"title": "idempotency-contract", "agent_type": "chat"}).get("session_id")
        if not session_id:
            raise RuntimeError("missing_session_id")
        return str(session_id)

    def delete_session(self, session_id: str) -> None:
        if self.request("DELETE", f"/sessions/{session_id}").get("status") != "deleted":
            raise RuntimeError("cleanup_delete_failed")
        try:
            self.request("GET", f"/sessions/{session_id}/messages")
        except RuntimeError as exc:
            if str(exc) == "api_40401":
                return
            raise RuntimeError("cleanup_verification_failed") from exc
        raise RuntimeError("cleanup_session_readable")


def run_replay_contract(client: Client, question: str) -> dict[str, Any]:
    if client.request("GET", "/me").get("tenant_id") != client.tenant_id:
        raise RuntimeError("tenant_mismatch")
    session_id = client.create_session()
    key = str(uuid.uuid4())
    payload = {"session_id": session_id, "question": question, "options": {"enable_rag": False, "enable_tools": False}}
    try:
        first = client.request("POST", "/chat", payload, {"Idempotency-Key": key})
        replay = client.request("POST", "/chat", payload, {"Idempotency-Key": key})
        if first != replay:
            return {"outcome": "replay_projection_mismatch"}
        if not first.get("trace_id"):
            return {"outcome": "replay_trace_missing"}
        conflict_payload = dict(payload)
        conflict_payload["question"] = question + "（不同请求）"
        try:
            client.request("POST", "/chat", conflict_payload, {"Idempotency-Key": key})
        except RuntimeError as exc:
            if str(exc) != f"api_{CONFLICT_CODE}":
                return {"outcome": "hash_conflict_wrong_error"}
        else:
            return {"outcome": "hash_conflict_accepted"}
        # The same durable response (including its one trace id) proves replay
        # used the completed turn rather than issuing a second externally
        # visible execution. Do not use global Prometheus counters here: other
        # legitimate online tests may share the model deployment concurrently.
        return {"outcome": "idempotency_replay_success", "trace_id": first["trace_id"], "response_hash": hashlib.sha256(json.dumps(first, sort_keys=True).encode()).hexdigest()[:12]}
    finally:
        client.delete_session(session_id)


def run_concurrent_contract(client: Client, question: str) -> dict[str, Any]:
    if client.request("GET", "/me").get("tenant_id") != client.tenant_id:
        raise RuntimeError("tenant_mismatch")
    session_id = client.create_session()
    key = str(uuid.uuid4())
    payload = {"session_id": session_id, "question": question, "options": {"enable_rag": False, "enable_tools": False}}
    barrier = threading.Barrier(2)

    def submit() -> tuple[str, str]:
        try:
            barrier.wait(timeout=10)
            response = client.request("POST", "/chat", payload, {"Idempotency-Key": key})
            trace_id = str(response.get("trace_id") or "")
            return ("success", trace_id)
        except RuntimeError as exc:
            return ("conflict", "") if str(exc) == f"api_{CONFLICT_CODE}" else ("unexpected_error", "")

    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            results = list(executor.map(lambda _: submit(), range(2)))
        outcomes = sorted(outcome for outcome, _ in results)
        trace_ids = [trace_id for outcome, trace_id in results if outcome == "success" and trace_id]
        serial = client.request("POST", "/chat", payload, {"Idempotency-Key": key})
        if not serial or not serial.get("trace_id"):
            return {"outcome": "serial_replay_missing"}
        if outcomes == ["conflict", "success"] and len(trace_ids) == 1 and serial["trace_id"] == trace_ids[0]:
            return {"outcome": "idempotency_concurrent_success", "trace_id": trace_ids[0], "request_outcomes": outcomes}
        return {"outcome": "concurrent_contract_failed", "successful_trace_count": len(trace_ids), "request_outcomes": outcomes}
    finally:
        client.delete_session(session_id)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run cleanup-safe synchronous Chat idempotency contract.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--tenant-id", default="default")
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument("--question", default=DEFAULT_QUESTION)
    parser.add_argument("--scenario", choices=["replay", "concurrent"], default="replay")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        client = Client(args.base_url, args.api_key, args.tenant_id, args.timeout)
        result = run_replay_contract(client, args.question) if args.scenario == "replay" else run_concurrent_contract(client, args.question)
    except RuntimeError as exc:
        print(json.dumps({"outcome": str(exc)}, sort_keys=True))
        return 1
    print(json.dumps(result, sort_keys=True))
    return 0 if result["outcome"] in {"idempotency_replay_success", "idempotency_concurrent_success"} else 1


if __name__ == "__main__":
    raise SystemExit(main())
