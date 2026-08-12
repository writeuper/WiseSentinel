#!/usr/bin/env python3
"""Run WiseSentinel Agent evaluation cases via HTTP APIs."""

import argparse
import csv
import json
import math
import re
import sys
import time
from collections import Counter
from pathlib import Path
from typing import Any, Dict, List, Tuple
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen

DEFAULT_INPUT = "docs/整理与提升/agent_eval_cases.csv"
DEFAULT_OUTPUT = "docs/整理与提升/agent_eval_results.csv"


class RateLimitError(RuntimeError):
    pass


class EvalClient:
    def __init__(self, base_url: str, api_key: str, timeout: int, retry_429: int = 3, retry_backoff: float = 2.0, interval: float = 0.0, tenant_id: str = "default", knowledge_tenant_id: str = "default", bearer_token: str = ""):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout
        self.retry_429 = retry_429
        self.retry_backoff = retry_backoff
        self.interval = interval
        self.tenant_id = tenant_id
        self.knowledge_tenant_id = knowledge_tenant_id
        self.bearer_token = bearer_token.strip()
        self._last_request_at = 0.0

    def _auth_headers(self) -> Dict[str, str]:
        # The tenant is an authenticated-identity property.  Do not use the
        # request header to claim a tenant that the credential cannot access.
        if self.bearer_token:
            return {"Authorization": f"Bearer {self.bearer_token}"}
        return {"X-API-Key": self.api_key}

    def _wait_interval(self) -> None:
        elapsed = time.time() - self._last_request_at
        if self.interval > elapsed:
            time.sleep(self.interval - elapsed)

    def _request(self, method: str, path: str, payload: Dict[str, Any] | None = None, tenant_id: str | None = None) -> Dict[str, Any]:
        self._wait_interval()
        url = f"{self.base_url}{path}"
        body = json.dumps(payload, ensure_ascii=False).encode("utf-8") if payload is not None else None
        headers = {"Accept": "application/json", "Content-Type": "application/json", **self._auth_headers()}
        if tenant_id:
            headers["X-Tenant-ID"] = tenant_id
        req = Request(url, data=body, headers=headers, method=method)
        self._last_request_at = time.time()
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
        except HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="ignore")
            if exc.code == 429:
                raise RateLimitError(f"HTTP 429 {url}: {raw}") from exc
            raise RuntimeError(f"HTTP {exc.code} {url}: {raw}") from exc
        except URLError as exc:
            raise RuntimeError(f"request failed {url}: {exc}") from exc
        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"invalid JSON response from {url}: {raw[:500]}") from exc
        if isinstance(envelope, dict) and "code" in envelope:
            if envelope.get("code") != 0:
                raise RuntimeError(f"API error {url}: {envelope}")
            data = envelope.get("data")
            return data if isinstance(data, dict) else {}
        return envelope if isinstance(envelope, dict) else {}

    def current_user(self) -> Dict[str, Any]:
        return self._request("GET", "/me")

    def post(self, path: str, payload: Dict[str, Any], tenant_id: str | None = None) -> Dict[str, Any]:
        for attempt in range(self.retry_429 + 1):
            try:
                return self._request("POST", path, payload, tenant_id=tenant_id)
            except RateLimitError:
                if attempt >= self.retry_429:
                    raise
                time.sleep(self.retry_backoff * (2 ** attempt))
        raise RuntimeError("unreachable")

    def create_session(self, case_id: str, tenant_id: str | None = None) -> str:
        data = self.post("/sessions", {"title": f"eval-{case_id}-{int(time.time() * 1000)}", "agent_type": "chat"}, tenant_id=tenant_id or self.tenant_id)
        session_id = data.get("session_id")
        if not session_id:
            raise RuntimeError(f"create session response missing session_id: {data}")
        return session_id

    def delete_session(self, session_id: str, tenant_id: str) -> None:
        deleted = self._request("DELETE", f"/sessions/{session_id}", tenant_id=tenant_id)
        if deleted.get("status") != "deleted":
            raise RuntimeError("session cleanup did not confirm deletion")
        try:
            self._request("GET", f"/sessions/{session_id}/messages", tenant_id=tenant_id)
        except RuntimeError as exc:
            if "HTTP 404" in str(exc):
                return
            raise RuntimeError("session cleanup verification failed") from exc
        raise RuntimeError("session remains readable after cleanup")

    def call_chat(self, case_id: str, question: str) -> Dict[str, Any]:
        session_id = self.create_session(case_id, tenant_id=self.tenant_id)
        try:
            return self.post("/chat", {"session_id": session_id, "question": question, "options": {"enable_rag": True, "enable_tools": True}}, tenant_id=self.tenant_id)
        finally:
            self.delete_session(session_id, self.tenant_id)

    def get_trace(self, trace_id: str, tenant_id: str | None = None) -> Dict[str, Any]:
        if not trace_id:
            return {}
        return self._request("GET", f"/traces/{trace_id}", tenant_id=tenant_id or self.tenant_id)

    def upload_knowledge(self, filename: str, content: str) -> Dict[str, Any]:
        for attempt in range(self.retry_429 + 1):
            try:
                return self._upload_knowledge_once(filename, content)
            except RateLimitError:
                if attempt >= self.retry_429:
                    raise
                time.sleep(self.retry_backoff * (2 ** attempt))
        raise RuntimeError("unreachable")

    def _upload_knowledge_once(self, filename: str, content: str) -> Dict[str, Any]:
        url = f"{self.base_url}/knowledge/documents/upload"
        boundary = "----WiseSentinelEvalBoundary"
        body = (f"--{boundary}\r\nContent-Disposition: form-data; name=\"file\"; filename=\"{filename}\"\r\nContent-Type: text/markdown\r\n\r\n{content}\r\n--{boundary}--\r\n").encode("utf-8")
        self._wait_interval()
        req = Request(url, data=body, headers={
            "Accept": "application/json",
            "Content-Type": f"multipart/form-data; boundary={boundary}",
            **self._auth_headers(),
            "X-Tenant-ID": self.knowledge_tenant_id,
        }, method="POST")
        self._last_request_at = time.time()
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
        except HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="ignore")
            if exc.code == 429:
                raise RateLimitError(f"HTTP 429 {url}: {raw}") from exc
            raise RuntimeError(f"HTTP {exc.code} {url}: {raw}") from exc
        except URLError as exc:
            raise RuntimeError(f"request failed {url}: {exc}") from exc
        envelope = json.loads(raw)
        if isinstance(envelope, dict) and envelope.get("code") != 0:
            raise RuntimeError(f"API error {url}: {envelope}")
        return envelope.get("data", {}) if isinstance(envelope, dict) else {}

    def get_index_task(self, task_id: str) -> Dict[str, Any]:
        return self._request("GET", f"/knowledge/index-tasks/{task_id}", tenant_id=self.knowledge_tenant_id)

    def delete_knowledge(self, doc_id: str) -> Dict[str, Any]:
        for attempt in range(self.retry_429 + 1):
            try:
                return self._request("DELETE", f"/knowledge/documents/{doc_id}", tenant_id=self.knowledge_tenant_id)
            except RateLimitError:
                if attempt >= self.retry_429:
                    raise
                time.sleep(self.retry_backoff * (2 ** attempt))
        raise RuntimeError("unreachable")

    def list_knowledge(self) -> Dict[str, Any]:
        return self._request("GET", "/knowledge/documents", tenant_id=self.knowledge_tenant_id)

    def call_ops(self, query: str, max_iterations: int) -> Dict[str, Any]:
        return self.post("/ops/analyze", {"query": query, "options": {"async": False, "max_iterations": max_iterations}}, tenant_id=self.tenant_id)


def split_pipe(value: str) -> List[str]:
    return [item.strip() for item in (value or "").split("|") if item.strip()]


def normalize_text(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    return json.dumps(value, ensure_ascii=False, sort_keys=True)


def summarize_output(text: str, max_len: int) -> str:
    # Evaluation artifacts are frequently attached to CI logs.  Keep enough
    # evidence to diagnose a failed assertion, but never persist credentials
    # that an upstream HTTP error or model/tool response may echo.
    text = re.sub(r"(?i)(authorization\s*[:=]\s*bearer\s+)[^\s,;]+", r"\1[REDACTED]", text or "")
    text = re.sub(r"(?i)(\b(?:api[_-]?key|token|password|secret)\b\s*[:=]\s*[\"']?)[^\s,;\"']+", r"\1[REDACTED]", text)
    compact = " ".join(text.split())
    return compact if len(compact) <= max_len else compact[:max_len] + "..."


def extract_trace_tool_calls(trace: Dict[str, Any]) -> Tuple[str, str]:
    """Return verified tool calls and a compact trace summary."""
    steps = trace.get("steps") or []
    tools: List[str] = []
    summary: List[str] = []
    for step in steps:
        if step.get("step_type") != "tool":
            continue
        name = step.get("step_name") or ""
        status = step.get("status") or "unknown"
        if name:
            tools.append(f"{name}:{status}")
        summary.append(normalize_text({
            "step_type": step.get("step_type"),
            "step_name": name,
            "status": status,
            "latency_ms": step.get("latency_ms"),
        }))
    return "|".join(sorted(set(tools))), "\n".join(summary)


def extract_chat_result(resp: Dict[str, Any], trace: Dict[str, Any] | None = None) -> Tuple[str, str, str, str, bool]:
    trace_data = trace or {}
    verified_tools, trace_summary = extract_trace_tool_calls(trace_data)
    trace_verified = bool(trace_data.get("trace") or trace_data.get("steps"))
    # Tool expectations must be checked against persisted tool steps, not only
    # model-reported tool_calls, so text-simulated calls cannot pass evaluation.
    tools = verified_tools
    citations = resp.get("citations") or []
    parts = [normalize_text(resp.get("answer"))]
    if citations:
        parts.append(normalize_text(citations))
    return "chat", tools, "\n".join(parts), trace_summary, trace_verified


def has_knowledge_workflow_evidence(answer: str, citations: Any) -> bool:
    evidence = answer + "\n" + normalize_text(citations)
    return "上传索引流程" in evidence or "query_internal_docs" in evidence


def run_knowledge_flow(client: EvalClient, case_id: str, timeout_seconds: int) -> Tuple[str, str, str]:
    marker = f"knowledge-eval-{case_id}-{int(time.time())}"
    uploaded = client.upload_knowledge(
        f"{marker}.md",
        f"# Knowledge Evaluation\n\n文档类型：知识库操作文档\nmarker: {marker}\n\n上传索引流程：调用 upload_knowledge 完成上传，等待索引任务成功后，通过 query_internal_docs 检索并生成 citation，最后调用 delete_knowledge 完成删除。\n\n上传、索引、检索和删除闭环验证。",
    )
    doc_id, task_id = uploaded.get("doc_id"), uploaded.get("task_id")
    if not doc_id or not task_id:
        raise RuntimeError("upload response missing doc_id/task_id")
    document_deleted = False
    try:
        deadline = time.time() + timeout_seconds
        task: Dict[str, Any] = {}
        not_found_attempts = 0
        while time.time() < deadline:
            try:
                task = client.get_index_task(task_id)
                not_found_attempts = 0
            except RuntimeError as exc:
                if "HTTP 404" not in str(exc):
                    raise
                not_found_attempts += 1
                if not_found_attempts >= 3:
                    raise RuntimeError("index task was not found after retries") from exc
                time.sleep(1)
                continue
            if task.get("status") in {"success", "failed"}:
                break
            time.sleep(1)
        if task.get("status") != "success" or int(task.get("chunk_count") or 0) <= 0:
            raise RuntimeError("knowledge index failed, timed out, or returned no chunks")
        session_id = client.create_session(case_id + "-rag", tenant_id=client.knowledge_tenant_id)
        try:
            chat = client.post("/chat", {"session_id": session_id, "question": f"请根据知识库说明 marker {marker} 的上传索引流程。", "options": {"enable_rag": True, "enable_tools": False}}, tenant_id=client.knowledge_tenant_id)
            citations = chat.get("citations") or []
            if not any(citation.get("doc_id") == doc_id for citation in citations):
                raise RuntimeError("RAG citation did not reference uploaded document")
            chat_text = normalize_text(chat.get("answer"))
            citation_text = normalize_text(citations)
            if marker not in chat_text and marker not in citation_text:
                raise RuntimeError("RAG response did not contain uploaded marker")
            if not has_knowledge_workflow_evidence(chat_text, citations):
                raise RuntimeError("RAG response did not contain knowledge workflow evidence")
        finally:
            client.delete_session(session_id, client.knowledge_tenant_id)
        deleted = client.delete_knowledge(doc_id)
        if deleted.get("status") != "deleted":
            raise RuntimeError(f"delete response invalid: {deleted}")
        document_deleted = True
        remaining = client.list_knowledge()
        if any(item.get("doc_id") == doc_id for item in remaining.get("items") or []):
            raise RuntimeError("deleted document still exists in list")
        return "knowledge", "upload_knowledge:success|index_task:success|chat_rag_citation:success|delete_knowledge:success", normalize_text({"upload": uploaded, "index": task, "chat": chat, "delete": deleted, "remaining": remaining})
    finally:
        if not document_deleted:
            cleanup = client.delete_knowledge(doc_id)
            if cleanup.get("status") != "deleted":
                raise RuntimeError("knowledge cleanup did not confirm deletion")


def extract_ops_result(resp: Dict[str, Any]) -> Tuple[str, str, str]:
    tools, output_parts = [], [normalize_text(resp.get("result")), normalize_text(resp.get("conclusion"))]
    for item in resp.get("evidence") or []:
        tool_name, status = item.get("tool_name"), item.get("status")
        if tool_name:
            tools.append(tool_name if not status else f"{tool_name}:{status}")
        if item.get("source"):
            output_parts.append(normalize_text({"source": item["source"]}))
    # Tool bodies and step details intentionally are not part of the public
    # response contract.  Tool success is verified by name/status; semantic
    # assertions use the controlled final result/conclusion only.
    return "ops", "|".join(sorted(set(tools))), "\n".join(part for part in output_parts if part)


def check_tools(expected_tools: str, actual_tools: str) -> Tuple[bool, int, int]:
    expected = split_pipe(expected_tools)
    if not expected:
        return True, 0, 0
    actual_status = {name: status or "unknown" for item in split_pipe(actual_tools) for name, _, status in [item.partition(":")]}
    hit = sum(1 for tool in expected if actual_status.get(tool) == "success")
    return hit == len(expected), hit, len(expected)


def check_forbidden_tools(forbidden_tools: str, actual_tools: str) -> Tuple[bool, int, int]:
    forbidden = set(split_pipe(forbidden_tools))
    if not forbidden:
        return True, 0, 0
    actual = {item.partition(":")[0] for item in split_pipe(actual_tools)}
    hit = len(forbidden & actual)
    return hit == 0, hit, len(forbidden)


def check_evidence_source(expected_source: str, actual_output: str) -> bool:
    expected = split_pipe(expected_source)
    if not expected:
        return True
    output = (actual_output or "").lower()
    return all(f'"source": "{item.lower()}"' in output or f"source: {item.lower()}" in output or item.lower() in output for item in expected)


def check_keywords(expected_keywords: str, actual_output: str, threshold: float) -> Tuple[bool, int, int]:
    expected = split_pipe(expected_keywords)
    if not expected:
        return True, 0, 0
    hit = sum(1 for keyword in expected if keyword.lower() in (actual_output or "").lower())
    return hit >= max(1, math.ceil(len(expected) * threshold)), hit, len(expected)


def check_knowledge(expected_knowledge: str, actual_output: str) -> Tuple[bool, int, int]:
    expected = split_pipe(expected_knowledge)
    if not expected:
        return True, 0, 0
    output = (actual_output or "").lower()
    hit = sum(1 for document in expected if document.lower() in output)
    return hit == len(expected), hit, len(expected)


def classify_failure(route_ok: bool, tools_ok: bool, knowledge_ok: bool, keywords_ok: bool, error: str) -> str:
    if error:
        lower_error = error.lower()
        if "429" in lower_error or "rate limit" in lower_error or "请求过于频繁" in error:
            return "rate_limited"
        if "timed out" in lower_error or "timeout" in lower_error:
            return "timeout"
        return "http_error"
    if not route_ok:
        return "route_error"
    if not tools_ok:
        return "tool_missing"
    if not knowledge_ok:
        return "knowledge_miss"
    if not keywords_ok:
        return "keyword_miss"
    return ""


def optimization_action(bad_case: str) -> str:
    return {"route_error": "调整路由规则、告警关键词或 RAG 置信度策略", "tool_missing": "优化 Planner Prompt 和工具 description，明确该场景应调用的工具", "forbidden_tool_called": "检查工具 Allowlist、tool_choice 策略和请求场景约束", "evidence_source_miss": "检查真实数据源标识、工具输出和 Evidence 记录", "trace_unavailable": "检查 trace_id 传播、Trace API 可用性和 tool Step 埋点",  "keyword_miss": "优化结论模板、工具返回格式或 Prompt 证据引用约束", "knowledge_miss": "检查 RAG 索引状态、citation 来源和知识库文档质量", "timeout": "降低 max_iterations，检查模型和工具超时配置", "http_error": "检查服务状态、鉴权配置、接口路径和依赖组件", "rate_limited": "降低评测速率或增加请求间隔，检查服务限流配置", "skipped": "补充对应 HTTP 入口后纳入自动评测"}.get(bad_case, "")


def run_case(client: EvalClient, row: Dict[str, str], args: argparse.Namespace) -> Dict[str, str]:
    expected_route = (row.get("expected_route") or "").strip().lower()
    actual_route = actual_tools = actual_output = trace_summary = error = ""
    trace_verified = True
    started = time.time()
    try:
        if expected_route == "chat":
            response = client.call_chat(row.get("case_id", ""), row.get("input", ""))
            trace_id = response.get("trace_id") or ""
            trace = client.get_trace(trace_id) if trace_id else {}
            actual_route, actual_tools, actual_output, trace_summary, trace_verified = extract_chat_result(response, trace)
        elif expected_route == "ops":
            actual_route, actual_tools, actual_output = extract_ops_result(client.call_ops(row.get("input", ""), args.max_iterations))
        elif expected_route == "knowledge":
            actual_route, actual_tools, actual_output = run_knowledge_flow(client, row.get("case_id", ""), args.timeout)
        else:
            actual_route, actual_output = "skipped", f"route {expected_route} is skipped by HTTP eval script"
    except Exception as exc:
        error, actual_route, actual_output = str(exc), "error", str(exc)
    route_ok = actual_route == expected_route
    tools_ok, tool_hit, tool_total = check_tools(row.get("expected_tools", ""), actual_tools)
    forbidden_ok, forbidden_hit, forbidden_total = check_forbidden_tools(row.get("forbidden_tools", ""), actual_tools)
    knowledge_ok, knowledge_hit, knowledge_total = check_knowledge(row.get("expected_knowledge", ""), actual_output)
    source_ok = check_evidence_source(row.get("expected_source", ""), actual_output)
    keywords_ok, keyword_hit, keyword_total = check_keywords(row.get("expected_keywords", ""), actual_output, args.keyword_threshold)
    bad_case = "skipped" if actual_route == "skipped" else classify_failure(route_ok, tools_ok and forbidden_ok and source_ok, knowledge_ok, keywords_ok, error)
    if not error and expected_route == "chat" and row.get("expected_tools") and not trace_verified:
        bad_case = "trace_unavailable"
    elif not error and not forbidden_ok:
        bad_case = "forbidden_tool_called"
    elif not error and not source_ok:
        bad_case = "evidence_source_miss"
    result = dict(row)
    result.update({"actual_route": actual_route, "actual_tools": actual_tools, "trace_summary": summarize_output(trace_summary, args.output_max_len), "actual_output": summarize_output(actual_output, args.output_max_len), "passed": "Y" if not bad_case else "N", "bad_case": bad_case, "optimization_action": optimization_action(bad_case), "latency_ms": str(int((time.time() - started) * 1000)), "tool_hit": f"{tool_hit}/{tool_total}" if tool_total else "", "forbidden_tool_hit": f"{forbidden_hit}/{forbidden_total}" if forbidden_total else "", "knowledge_hit": f"{knowledge_hit}/{knowledge_total}" if knowledge_total else "", "keyword_hit": f"{keyword_hit}/{keyword_total}" if keyword_total else ""})
    return result


def load_resume_results(path: Path) -> Dict[str, Dict[str, str]]:
    """Load prior case results for safe, idempotent batch resumption."""
    if not path.exists():
        return {}
    try:
        with path.open("r", encoding="utf-8-sig", newline="") as file:
            rows = csv.DictReader(file)
            return {str(row.get("case_id") or ""): dict(row) for row in rows if row.get("case_id")}
    except (OSError, csv.Error):
        return {}


def read_cases(path: Path) -> Tuple[List[str], List[Dict[str, str]]]:
    with path.open("r", encoding="utf-8-sig", newline="") as file:
        reader = csv.DictReader(file)
        return list(reader.fieldnames or []), list(reader)


def write_results(path: Path, fieldnames: List[str], rows: List[Dict[str, str]]) -> None:
    output_fields = list(fieldnames)
    for field in ["latency_ms", "tool_hit", "forbidden_tool_hit", "knowledge_hit", "keyword_hit", "trace_summary"]:
        if field not in output_fields:
            output_fields.append(field)
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8-sig", newline="") as file:
        writer = csv.DictWriter(file, fieldnames=output_fields, extrasaction="ignore")
        writer.writeheader()
        writer.writerows(rows)


def build_metrics(rows: List[Dict[str, str]]) -> Dict[str, Any]:
    executable = [row for row in rows if row.get("actual_route") != "skipped"]
    passed = [row for row in executable if row.get("passed") == "Y"]
    route_ok = [row for row in executable if row.get("actual_route") == row.get("expected_route")]
    tool_hit = tool_total = keyword_hit = keyword_total = 0
    for row in executable:
        if row.get("tool_hit"):
            hit, total = row["tool_hit"].split("/", 1)
            tool_hit, tool_total = tool_hit + int(hit), tool_total + int(total)
        if row.get("keyword_hit"):
            hit, separator, total = row["keyword_hit"].partition("/")
            if separator:
                keyword_hit += int(hit)
                keyword_total += int(total)
    latencies = sorted(int(row.get("latency_ms") or 0) for row in executable)
    def percentile(percent: float) -> int | None:
        if not latencies:
            return None
        index = max(0, math.ceil(len(latencies) * percent) - 1)
        return latencies[index]
    return {
        "total_cases": len(rows),
        "executed_cases": len(executable),
        "passed_cases": len(passed),
        "overall_pass_rate": len(passed) / len(executable) if executable else None,
        "route_accuracy": len(route_ok) / len(executable) if executable else None,
        "tool_success_rate": tool_hit / tool_total if tool_total else None,
        "tool_hits": tool_hit,
        "tool_total": tool_total,
        "keyword_hit_rate": keyword_hit / keyword_total if keyword_total else None,
        "keyword_hits": keyword_hit,
        "keyword_total": keyword_total,
        "latency_ms": {"p50": percentile(0.50), "p95": percentile(0.95)},
        "bad_case_distribution": dict(Counter(row.get("bad_case") or "pass" for row in rows)),
    }


def print_metrics(metrics: Dict[str, Any]) -> None:
    def pct(value: float | None) -> str:
        return "N/A" if value is None else f"{value * 100:.1f}%"
    print("\n=== Agent Eval Summary ===")
    print(f"total cases: {metrics['total_cases']}")
    print(f"executed cases: {metrics['executed_cases']}")
    print(f"overall pass rate: {pct(metrics['overall_pass_rate'])} ({metrics['passed_cases']}/{metrics['executed_cases']})")
    print(f"route accuracy: {pct(metrics['route_accuracy'])}")
    print(f"tool success rate: {pct(metrics['tool_success_rate'])} ({metrics['tool_hits']}/{metrics['tool_total']})")
    print(f"keyword hit rate: {pct(metrics['keyword_hit_rate'])} ({metrics['keyword_hits']}/{metrics['keyword_total']})")
    print(f"latency: p50={metrics['latency_ms']['p50']}ms p95={metrics['latency_ms']['p95']}ms")
    print("bad case distribution:")
    for name, count in sorted(metrics["bad_case_distribution"].items(), key=lambda item: (-item[1], item[0])):
        print(f"  {name}: {count}")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run WiseSentinel Agent eval cases via HTTP APIs.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", default="ws-dev-key")
    parser.add_argument("--bearer-token", default="", help="Pre-provisioned test JWT; takes precedence over --api-key.")
    parser.add_argument("--input", default=DEFAULT_INPUT)
    parser.add_argument("--output", default=DEFAULT_OUTPUT)
    parser.add_argument("--timeout", type=int, default=180)
    parser.add_argument("--max-iterations", type=int, default=20)
    parser.add_argument("--keyword-threshold", type=float, default=0.5)
    parser.add_argument("--output-max-len", type=int, default=1000)
    parser.add_argument("--retry-429", type=int, default=3)
    parser.add_argument("--retry-backoff", type=float, default=2.0)
    parser.add_argument("--interval", type=float, default=0.0)
    parser.add_argument("--tenant-id", default="default")
    parser.add_argument("--knowledge-tenant-id", default="default")
    parser.add_argument("--only", choices=["chat", "ops", "knowledge"], default="")
    parser.add_argument("--case", default="")
    parser.add_argument("--limit", type=int, default=0)
    parser.add_argument("--summary-json", default="", help="Write aggregate metrics only; contains no model/tool output.")
    parser.add_argument("--allow-failures", action="store_true", help="Report failed cases without returning a non-zero status (diagnostics only).")
    parser.add_argument("--resume", action="store_true", help="Resume from --output and skip cases that already passed; retry prior failures.")
    return parser.parse_args()


def validate_tenant_binding(client: EvalClient, requested_tenants: List[str]) -> str:
    identity = client.current_user()
    authenticated_tenant = str(identity.get("tenant_id") or "").strip()
    if not authenticated_tenant:
        raise RuntimeError("authenticated test identity does not contain tenant_id")
    if any(tenant != authenticated_tenant for tenant in requested_tenants):
        raise RuntimeError("requested tenant does not match authenticated test identity")
    return authenticated_tenant


def main() -> int:
    args = parse_args()
    input_path, output_path = Path(args.input), Path(args.output)
    if not input_path.exists():
        print(f"input file not found: {input_path}", file=sys.stderr)
        return 1
    fieldnames, cases = read_cases(input_path)
    if args.only:
        cases = [case for case in cases if (case.get("expected_route") or "").lower() == args.only]
    if args.case:
        cases = [case for case in cases if case.get("case_id") == args.case]
    if args.limit > 0:
        cases = cases[:args.limit]
    if not cases:
        print("no evaluation cases selected", file=sys.stderr)
        return 2
    client = EvalClient(args.base_url, args.api_key, args.timeout, args.retry_429, args.retry_backoff, args.interval, args.tenant_id, args.knowledge_tenant_id, args.bearer_token)
    try:
        validate_tenant_binding(client, [args.tenant_id, args.knowledge_tenant_id])
    except RuntimeError as exc:
        print(str(exc), file=sys.stderr)
        return 2
    prior_results = load_resume_results(output_path) if args.resume else {}
    results = []
    skipped = 0
    for index, case in enumerate(cases, start=1):
        prior = prior_results.get(case.get("case_id", ""))
        if prior and prior.get("passed") == "Y":
            results.append(prior)
            skipped += 1
            print(f"[{index}/{len(cases)}] {case.get('case_id')} resumed=Y passed={prior.get('passed')} bad_case={prior.get('bad_case')}")
            continue
        result = run_case(client, case, args)
        print(f"[{index}/{len(cases)}] {case.get('case_id')} route={result['actual_route']} passed={result['passed']} bad_case={result['bad_case']}")
        results.append(result)
        # Persist after every case so an upstream timeout or process restart
        # loses at most one case and can be resumed deterministically.
        write_results(output_path, fieldnames, results)
    write_results(output_path, fieldnames, results)
    metrics = build_metrics(results)
    print_metrics(metrics)
    if args.resume:
        print(f"resumed cases: {skipped}")
    if args.summary_json:
        summary_path = Path(args.summary_json)
        summary_path.parent.mkdir(parents=True, exist_ok=True)
        with summary_path.open("w", encoding="utf-8") as file:
            json.dump(metrics, file, ensure_ascii=False, indent=2, sort_keys=True)
            file.write("\n")
        print(f"aggregate metrics saved to: {summary_path}")
    print(f"\nresult saved to: {output_path}")
    if metrics["executed_cases"] == 0 or metrics["passed_cases"] != metrics["executed_cases"]:
        return 0 if args.allow_failures else 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
