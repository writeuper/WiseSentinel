#!/usr/bin/env python3
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
result = subprocess.run([sys.executable, "scripts/validate_baseline_artifacts.py"], cwd=ROOT, text=True, capture_output=True)
assert result.returncode == 0, result.stderr
assert "baseline valid:" in result.stdout
print("baseline validator smoke test passed")
