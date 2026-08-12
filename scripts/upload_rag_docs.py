#!/usr/bin/env python3
"""Batch upload RAG knowledge documents to WiseSentinel."""

import argparse
import sys
import time
import urllib.parse
from pathlib import Path
from typing import Dict, List, Optional, Tuple
from urllib.error import HTTPError, URLError
from urllib.request import Request, urlopen


class KnowledgeClient:
    def __init__(self, base_url: str, api_key: str, timeout: int):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.timeout = timeout

    def _api_request(self, method: str, path: str, body: Optional[bytes] = None,
                     extra_headers: Optional[Dict[str, str]] = None) -> Dict:
        url = f"{self.base_url}{path}"
        headers = {"X-API-Key": self.api_key}
        if extra_headers:
            headers.update(extra_headers)

        req = Request(url, data=body, headers=headers, method=method)
        try:
            with urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
        except HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="ignore")
            raise RuntimeError(f"HTTP {exc.code} {url}: {raw}") from exc
        except URLError as exc:
            raise RuntimeError(f"request failed {url}: {exc}") from exc

        import json

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

    def list_documents(self) -> List[Dict]:
        """List existing knowledge documents."""
        data = self._api_request("GET", "/knowledge/documents?page=1&size=200")
        return data.get("items") or []

    def delete_document(self, doc_id: str) -> Dict:
        """Delete a knowledge document by id."""
        return self._api_request("DELETE", f"/knowledge/documents/{doc_id}")

    def upload_document(self, file_path: Path, visibility: str = "tenant",
                        secret_level: int = 1) -> Dict:
        """Upload a document (multipart/form-data)."""
        boundary, body = build_multipart_upload(file_path, visibility, secret_level)

        url = f"{self.base_url}/knowledge/documents/upload"
        import urllib.request
        req = urllib.request.Request(url, data=body, method="POST")
        req.add_header("X-API-Key", self.api_key)
        req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")

        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8")
        except HTTPError as exc:
            raw = exc.read().decode("utf-8", errors="ignore")
            raise RuntimeError(f"HTTP {exc.code} {url}: {raw}") from exc
        except URLError as exc:
            raise RuntimeError(f"request failed {url}: {exc}") from exc

        import json
        try:
            envelope = json.loads(raw)
        except json.JSONDecodeError as exc:
            raise RuntimeError(f"invalid JSON response from {url}: {raw[:500]}") from exc

        if isinstance(envelope, dict) and "code" in envelope:
            if envelope.get("code") != 0:
                raise RuntimeError(f"API error {url}: {envelope}")
            data = envelope.get("data")
            return data if isinstance(data, dict) else {}
        return {}

    def get_index_task(self, task_id: str) -> Dict:
        """Query index task status."""
        return self._api_request("GET", f"/knowledge/index-tasks/{task_id}")

    def wait_for_index(self, task_id: str, max_wait: int = 120, poll_interval: int = 3) -> Tuple[str, int]:
        """Wait for an index task to complete."""
        elapsed = 0
        while elapsed < max_wait:
            task = self.get_index_task(task_id)
            status = task.get("status", "")
            if status == "success":
                return status, task.get("chunk_count", 0)
            if status == "failed":
                return status, 0
            time.sleep(poll_interval)
            elapsed += poll_interval
        return "timeout", 0


def scan_docs(doc_dir: Path) -> List[Path]:
    """Scan directory for .md files, sorted by name."""
    files = sorted(doc_dir.glob("*.md"))
    return [f for f in files if f.name != "README.md"]


def build_multipart_upload(file_path: Path, visibility: str, secret_level: int) -> Tuple[str, bytes]:
    """Build a raw multipart body without MIME transfer encoding the document.

    ``email.mime.MIMEApplication`` emits Content-Transfer-Encoding: base64.
    HTTP multipart file parts are binary-safe already, so sending that encoded
    representation made the server persist and embed Base64 text instead of
    the source Markdown. Keep the original bytes intact and make the wire
    format independently testable.
    """
    boundary = "----WiseSentinelUploadBoundary"
    content = file_path.read_bytes()
    filename = file_path.name.replace("\\", "_").replace('"', "_")
    encoded_filename = urllib.parse.quote(filename.encode("utf-8"), safe="")
    marker = f"--{boundary}\r\n".encode("ascii")
    chunks = [
        marker,
        (
            f'Content-Disposition: form-data; name="file"; filename="{filename}"; '
            f"filename*=UTF-8''{encoded_filename}\r\n"
        ).encode("utf-8"),
        b"Content-Type: application/octet-stream\r\n\r\n",
        content,
        b"\r\n",
        marker,
        b'Content-Disposition: form-data; name="visibility"\r\n\r\n',
        visibility.encode("utf-8"),
        b"\r\n",
        marker,
        b'Content-Disposition: form-data; name="secret_level"\r\n\r\n',
        str(secret_level).encode("ascii"),
        b"\r\n",
        f"--{boundary}--\r\n".encode("ascii"),
    ]
    return boundary, b"".join(chunks)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Batch upload RAG documents to WiseSentinel knowledge base.")
    parser.add_argument("--base-url", default="http://127.0.0.1:8090/api/v1")
    parser.add_argument("--api-key", default="ws-dev-key")
    parser.add_argument("--doc-dir", default="docs/整理与提升/rag_eval_docs")
    parser.add_argument("--timeout", type=int, default=120)
    parser.add_argument("--wait-index", action="store_true", default=True,
                        help="Wait for each index task to complete after upload")
    parser.add_argument("--no-wait", action="store_false", dest="wait_index",
                        help="Do not wait for index tasks, just upload")
    parser.add_argument("--clear-existing", action="store_true",
                        help="Delete all existing documents before uploading")
    parser.add_argument("--include", action="append", default=[], metavar="FILENAME",
                        help="Upload only a named document; may be repeated")
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    doc_dir = Path(args.doc_dir)
    if not doc_dir.is_dir():
        print(f"doc directory not found: {doc_dir}", file=sys.stderr)
        return 1

    client = KnowledgeClient(args.base_url, args.api_key, args.timeout)
    files = scan_docs(doc_dir)
    if args.include:
        wanted = set(args.include)
        files = [file_path for file_path in files if file_path.name in wanted]
    if not files:
        print(f"no .md files found in {doc_dir}", file=sys.stderr)
        return 1

    print(f"found {len(files)} documents to upload:\n")
    for f in files:
        size_kb = f.stat().st_size / 1024
        print(f"  {f.name}  ({size_kb:.1f} KB)")
    print()

    # Optionally clear existing documents
    if args.clear_existing:
        existing = client.list_documents()
        if existing:
            print(f"clearing {len(existing)} existing documents...")
            for doc in existing:
                doc_id = doc.get("doc_id")
                if doc_id:
                    try:
                        client.delete_document(doc_id)
                        print(f"  deleted {doc.get('name', doc_id)}")
                    except RuntimeError as exc:
                        print(f"  failed to delete {doc.get('name', doc_id)}: {exc}", file=sys.stderr)
            print()

    # Upload
    success = 0
    failed = 0
    results = []

    for i, file_path in enumerate(files, start=1):
        print(f"[{i}/{len(files)}] uploading {file_path.name} ...", end=" ", flush=True)
        try:
            resp = client.upload_document(file_path)
            doc_id = resp.get("doc_id", "")
            task_id = resp.get("task_id", "")
            status = resp.get("status", "uploaded")
            print(f"doc_id={doc_id} task_id={task_id} status={status}")

            if args.wait_index and task_id:
                print(f"           waiting for indexing {task_id} ...", end=" ", flush=True)
                idx_status, chunk_count = client.wait_for_index(task_id)
                print(f"index={idx_status} chunks={chunk_count}")
                status = idx_status

            results.append({
                "file": file_path.name,
                "doc_id": doc_id,
                "task_id": task_id,
                "status": status,
            })
            if status in ("uploaded", "success"):
                success += 1
            else:
                failed += 1
        except RuntimeError as exc:
            print(f"FAILED: {exc}")
            results.append({
                "file": file_path.name,
                "doc_id": "",
                "task_id": "",
                "status": "error",
            })
            failed += 1

    # Summary
    print(f"\n{'='*50}")
    print(f"Upload complete: {success} succeeded, {failed} failed")
    print(f"{'='*50}")
    for r in results:
        flag = "OK" if r["status"] in ("uploaded", "success") else "FAIL"
        print(f"  [{flag}] {r['file']}  doc_id={r['doc_id']}  task_id={r['task_id']}  status={r['status']}")

    return 0 if failed == 0 else 1


if __name__ == "__main__":
    raise SystemExit(main())
