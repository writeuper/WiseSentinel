#!/usr/bin/env python3
"""Guard the portal's initial JavaScript entry size after a production build."""

from __future__ import annotations

import re
import unittest
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
INDEX = ROOT / "portal" / "dist" / "index.html"
MAX_INITIAL_JS_BYTES = 500 * 1024


class PortalBundleTests(unittest.TestCase):
    def test_initial_script_is_split_below_warning_threshold(self) -> None:
        self.assertTrue(INDEX.exists(), "run `npm run build` before bundle checks")
        html = INDEX.read_text(encoding="utf-8")
        scripts = re.findall(r'<script[^>]+src="([^"]+\.js)"', html)
        self.assertTrue(scripts, "production index must reference an entry script")
        entry = (INDEX.parent / scripts[0].removeprefix("/" )).resolve()
        self.assertTrue(entry.exists(), f"missing entry asset: {entry}")
        self.assertLess(
            entry.stat().st_size,
            MAX_INITIAL_JS_BYTES,
            f"initial entry is {entry.stat().st_size} bytes; split vendor dependencies",
        )


if __name__ == "__main__":
    unittest.main()
