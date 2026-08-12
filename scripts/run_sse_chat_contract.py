#!/usr/bin/env python3
"""Cleanup-safe live contracts for the WiseSentinel Chat SSE API.

Only aggregate event categories are printed. The script never writes API keys,
questions, model text, citation snippets, or raw HTTP error bodies.
"""

import argparse
import concurrent.futures
import json
import sys
import threading
import time
from collections import Counter
from typing import Any
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


DEFAULT_QUESTION = "请简要说明企业级 Agent 平台健康检查应覆盖哪些基础组件。"


class SSEClient:
    def __init__(self, base_url: str, api_key: str, bearer_token: str, tenant_id: str, timeout: int) -> None:
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.bearer_token = bearer_token.strip()
        self.tenant_id = tenant_id
        self.timeout = timeout

    def _headers(self, stream: bool = False) -> dict[str, str]:
        headers = {"Accept": "text/event-stream" if stream else "application/json", "Content-Type": "application/json", "X-Tenant-ID": self.tenant_id}
        headers["Authorization" if self.bearer_token else "X-API-Key"] = f"Bearer {self.bearer_token}" if self.bearer_token else self.api_key
        return headers

    def request(self, method: str, path: str, payload: dict[str, Any] | None = None) -> dict[str, Any]:
        data = json.dumps(payload, ensure_ascii=False).encode() if payload is not None else None
        request = Request(self.base_url + path, data=data, headers=self._headers(), method=method)
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

    def current_user(self) -> dict[str, Any]:
        return self.request("GET", "/me")

    def create_session(self, suffix: str) -> str:
        session_id = self.request("POST", "/sessions", {"title": f"sse-contract-{suffix}", "agent_type": "chat"}).get("session_id")
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

    def read_stream(self, session_id: str, question: str, cancel_on_message: bool = False, on_event: Any = None) -> list[tuple[str, str]]:
        body = json.dumps({"session_id": session_id, "question": question, "options": {"enable_rag": False, "enable_tools": False}}, ensure_ascii=False).encode()
        request = Request(self.base_url + "/chat/stream", data=body, headers=self._headers(stream=True), method="POST")
        events: list[tuple[str, str]] = []
        try:
            with urlopen(request, timeout=self.timeout) as response:
                event = ""
                for raw in response:
                    line = raw.decode().rstrip("\r\n")
                    if line.startswith("event: "):
                        event = line[7:]
                    elif line.startswith("data: "):
                        events.append((event, line[6:]))
                        if on_event is not None:
                            on_event(event)
                        if cancel_on_message and event == "message":
                            return events
        except HTTPError as exc:
            raw = exc.read().decode(errors="ignore")
            try:
                events.append(("http_error", str(json.loads(raw).get("code"))))
            except (json.JSONDecodeError, AttributeError):
                events.append(("http_error", str(exc.code)))
        return events


def stream_outcome(events: list[tuple[str, str]]) -> str:
    for event, payload in events:
        if event == "error":
            try:
                decoded = json.loads(payload)
            except json.JSONDecodeError:
                return "sse_error"
            if decoded.get("code") == 50304 and decoded.get("retry_after_seconds") == 2:
                return "sse_50304_retry_2"
            return "sse_error"
        if event == "http_error":
            return f"http_{payload}"
    names = [event for event, _ in events]
    return "success_done" if "connected" in names and "message" in names and "done" in names else "stream_incomplete"


def run_basic(client: SSEClient, question: str) -> dict[str, Any]:
    session_id = client.create_session("basic")
    try:
        return {"outcome": stream_outcome(client.read_stream(session_id, question))}
    finally:
        client.delete_session(session_id)


def run_cancel(client: SSEClient, question: str) -> dict[str, Any]:
    session_id = client.create_session("cancel")
    try:
        events = client.read_stream(session_id, question, cancel_on_message=True)
        if not any(event == "message" for event, _ in events):
            return {"outcome": "cancel_no_message"}
        time.sleep(2)
        messages = client.request("GET", f"/sessions/{session_id}/messages").get("messages") or []
        return {"outcome": "cancel_not_persisted" if not messages else "cancel_persisted"}
    finally:
        client.delete_session(session_id)


def run_delete_race(client: SSEClient, question: str) -> dict[str, Any]:
    """Delete immediately after SSE connected and assert no clean completion.

    This exercises the production-facing race between a user deleting a chat
    and a model response that is still in flight. No event payload or history
    body is reported by the harness.
    """
    session_id = client.create_session("delete-race")
    deleted = False

    def delete_on_connect(event: str) -> None:
        nonlocal deleted
        if event == "connected" and not deleted:
            client.delete_session(session_id)
            deleted = True

    try:
        events = client.read_stream(session_id, question, on_event=delete_on_connect)
        names = [event for event, _ in events]
        try:
            client.request("GET", f"/sessions/{session_id}/messages")
        except RuntimeError as exc:
            if str(exc) != "api_40401":
                return {"outcome": "delete_race_history_check_failed"}
        else:
            return {"outcome": "delete_race_session_readable"}
        if deleted and "done" not in names:
            return {"outcome": "delete_race_not_completed"}
        return {"outcome": "delete_race_unexpected_completion"}
    finally:
        # The connected callback normally deletes the session. If a transport
        # failure happened before it, retain the normal exact-resource cleanup.
        if not deleted:
            client.delete_session(session_id)


def run_capacity(client: SSEClient, question: str, requests: int) -> dict[str, Any]:
    sessions = [client.create_session(f"capacity-{index}") for index in range(requests)]
    barrier = threading.Barrier(requests)
    def one(session_id: str) -> str:
        try:
            barrier.wait(timeout=20)
            return stream_outcome(client.read_stream(session_id, question))
        except threading.BrokenBarrierError:
            return "barrier_error"
    try:
        with concurrent.futures.ThreadPoolExecutor(max_workers=requests) as executor:
            outcomes = list(executor.map(one, sessions))
        return {"outcomes": dict(sorted(Counter(outcomes).items()))}
    finally:
        for session_id in sessions:
            client.delete_session(session_id)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run cleanup-safe live SSE Chat contracts.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", required=True)
    parser.add_argument("--bearer-token", default="")
    parser.add_argument("--tenant-id", default="default")
    parser.add_argument("--scenario", choices=["basic", "cancel", "delete-race", "capacity"], default="basic")
    parser.add_argument("--requests", type=int, default=10)
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument("--question", default=DEFAULT_QUESTION)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.requests < 2 and args.scenario == "capacity":
        print("capacity scenario requires --requests >= 2", file=sys.stderr)
        return 2
    client = SSEClient(args.base_url, args.api_key, args.bearer_token, args.tenant_id, args.timeout)
    identity = client.current_user()
    if identity.get("tenant_id") != args.tenant_id:
        print("requested tenant does not match authenticated test identity", file=sys.stderr)
        return 2
    if args.scenario == "basic":
        result = run_basic(client, args.question)
    elif args.scenario == "cancel":
        result = run_cancel(client, args.question)
    elif args.scenario == "delete-race":
        result = run_delete_race(client, args.question)
    else:
        result = run_capacity(client, args.question, args.requests)
    print(json.dumps(result, ensure_ascii=False, sort_keys=True))
    if args.scenario == "basic":
        return 0 if result["outcome"] == "success_done" else 1
    if args.scenario == "cancel":
        return 0 if result["outcome"] == "cancel_not_persisted" else 1
    if args.scenario == "delete-race":
        return 0 if result["outcome"] == "delete_race_not_completed" else 1
    outcomes = result["outcomes"]
    return 0 if outcomes.get("success_done", 0) > 0 and outcomes.get("sse_50304_retry_2", 0) > 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
