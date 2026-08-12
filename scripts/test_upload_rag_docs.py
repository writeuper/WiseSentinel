#!/usr/bin/env python3
"""Regression tests for the evaluation-corpus multipart uploader."""

import importlib.util
import tempfile
import unittest
from pathlib import Path


def load_uploader_module():
    script = Path(__file__).with_name("upload_rag_docs.py")
    spec = importlib.util.spec_from_file_location("upload_rag_docs", script)
    assert spec and spec.loader
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


class MultipartUploadTest(unittest.TestCase):
    def test_preserves_raw_markdown_bytes_without_transfer_encoding(self):
        uploader = load_uploader_module()
        original = "# 订单服务处置手册\n\n关键字：mysql deadlock\n".encode("utf-8")
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "07-订单服务处置手册.md"
            source.write_bytes(original)
            boundary, body = uploader.build_multipart_upload(source, "tenant", 1)

        self.assertIn(original, body)
        self.assertNotIn(b"Content-Transfer-Encoding", body)
        self.assertNotIn(b"IyDorqLljZXmnI3liqHlpITnva7miYvmiYsi", body)
        self.assertIn(f"--{boundary}".encode("ascii"), body)
        self.assertIn(b'name="visibility"\r\n\r\ntenant', body)
        self.assertIn(b'name="secret_level"\r\n\r\n1', body)


if __name__ == "__main__":
    unittest.main()
