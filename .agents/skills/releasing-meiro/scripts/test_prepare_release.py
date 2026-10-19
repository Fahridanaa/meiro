#!/usr/bin/env python3
"""Exercise release preparation only against an isolated fixture."""

from __future__ import annotations

import json
import os
import subprocess
import sys
import tempfile
from pathlib import Path


SCRIPT = Path(__file__).with_name("prepare_release.py")
FIXTURE_CHANGELOG = """# Changelog

## [Unreleased]

### Fixed

- Keep the user's unreleased note.

## [0.4.1] - 2026-10-19

- Older note.
"""
FIXTURE_MODULE = 'module(name = "meiro",\n    version = "0.4.1",\n)\n'
FIXTURE_CONFIG = {"name": "Meiro", "version": "0.4.1"}


def run(*args: str) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, "-B", str(SCRIPT), *args],
        check=False,
        capture_output=True,
        text=True,
        env={**os.environ, "PYTHONDONTWRITEBYTECODE": "1"},
    )


def main() -> None:
    with tempfile.TemporaryDirectory(prefix="meiro-release-fixture-") as directory:
        root = Path(directory)
        (root / "CHANGELOG.md").write_text(FIXTURE_CHANGELOG, encoding="utf-8")
        (root / "MODULE.bazel").write_text(FIXTURE_MODULE, encoding="utf-8")
        (root / "mygo.json").write_text(json.dumps(FIXTURE_CONFIG) + "\n", encoding="utf-8")
        originals = {name: (root / name).read_bytes() for name in ("CHANGELOG.md", "MODULE.bazel", "mygo.json")}

        dry_run = run("0.5.0", "--date", "2026-10-20", "--root", str(root))
        assert dry_run.returncode == 0, dry_run.stderr
        assert "Dry run only" in dry_run.stdout
        assert originals == {name: (root / name).read_bytes() for name in originals}

        applied = run("0.5.0", "--date", "2026-10-20", "--root", str(root), "--apply")
        assert applied.returncode == 0, applied.stderr
        assert "Prepared and verified" in applied.stdout
        changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
        assert "## [0.5.0] - 2026-10-20" in changelog
        assert "- Keep the user's unreleased note." in changelog
        assert "## [0.4.1] - 2026-10-19" in changelog
        module = (root / "MODULE.bazel").read_text(encoding="utf-8")
        assert 'version = "0.5.0"' in module
        config = json.loads((root / "mygo.json").read_text(encoding="utf-8"))
        assert config["version"] == "0.5.0"

        verified = run("0.5.0", "--root", str(root), "--verify")
        assert verified.returncode == 0, verified.stderr
        assert "Verified Meiro 0.5.0" in verified.stdout
        assert not list(root.rglob("__pycache__"))

    print("release-prep fixture: dry run is read-only; apply and verify pass; no bytecode created")


if __name__ == "__main__":
    main()
