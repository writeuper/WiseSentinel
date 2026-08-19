#!/usr/bin/env python3
"""Deterministic OpenAI-compatible provider for local contract smoke tests."""
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import os

class Handler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path not in ("/v1/chat/completions", "/chat/completions"):
            self.send_error(404)
            return
        length = int(self.headers.get("content-length", "0"))
        try:
            request = json.loads(self.rfile.read(length) or b"{}")
        except json.JSONDecodeError:
            self.send_error(400)
            return
        tools = request.get("tools") or []
        names = [item.get("function", {}).get("name") for item in tools]
        if "task_complete" in names and request.get("tool_choice") in ("required", "forced"):
            args = {"status": "completed", "summary": "mock provider completion", "checklist": [{"item": "successful tool or citation evidence", "completed": True}, {"item": "summary", "completed": True}]}
            message = {"role": "assistant", "content": "", "tool_calls": [{"id": "mock-task-complete", "type": "function", "function": {"name": "task_complete", "arguments": json.dumps(args)}}]}
        else:
            message = {"role": "assistant", "content": "mock provider response"}
        payload = {"choices": [{"message": message, "finish_reason": "tool_calls" if message.get("tool_calls") else "stop"}], "usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}}
        encoded = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)
    def log_message(self, *_):
        return

ThreadingHTTPServer(("0.0.0.0", int(os.environ.get("PORT", "8081"))), Handler).serve_forever()
