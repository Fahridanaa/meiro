#!/usr/bin/env python3
"""Move the Unreleased changelog section into a dated Meiro release."""

from __future__ import annotations

import argparse
import json
import re
from datetime import date
from pathlib import Path


ROOT = Path(__file__).resolve().parents[4]
CHANGELOG = ROOT / "CHANGELOG.md"
MODULE = ROOT / "MODULE.bazel"
APP_CONFIG = ROOT / "mygo.json"
VERSION_PATTERN = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")


def parse_version(value: str) -> tuple[int, int, int]:
    match = VERSION_PATTERN.fullmatch(value)
    if match is None:
        raise ValueError(f"Version must be numeric x.y.z without a prefix: {value}")
    return tuple(map(int, match.groups()))


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version without a v prefix")
    parser.add_argument("--date", default=date.today().isoformat(), help="release date (YYYY-MM-DD)")
    args = parser.parse_args()

    try:
        target = parse_version(args.version)
    except ValueError as error:
        parser.error(str(error))
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}", args.date) is None:
        parser.error(f"Invalid release date: {args.date}; expected YYYY-MM-DD.")
    try:
        release_date = date.fromisoformat(args.date)
    except ValueError as error:
        parser.error(f"Invalid release date: {args.date}.")

    changelog = CHANGELOG.read_text(encoding="utf-8")
    module = MODULE.read_text(encoding="utf-8")
    app_config = json.loads(APP_CONFIG.read_text(encoding="utf-8"))

    module_match = re.search(r'^    version = "([^"]+)",$', module, re.MULTILINE)
    if module_match is None:
        raise SystemExit("Could not find the version in MODULE.bazel.")
    current_versions = (module_match.group(1), app_config.get("version"))
    if current_versions[0] != current_versions[1]:
        raise SystemExit(
            "Version mismatch: MODULE.bazel has "
            f"{current_versions[0]}, mygo.json has {current_versions[1]}."
        )
    current = parse_version(current_versions[0])
    if target <= current:
        raise SystemExit(f"New version {args.version} must be greater than {current_versions[0]}.")

    release_header = f"## [{args.version}]"
    if re.search(rf"^{re.escape(release_header)}(?:\s|$)", changelog, re.MULTILINE):
        raise SystemExit(f"CHANGELOG.md already has a section for {args.version}.")
    unreleased_match = re.search(r"^## \[Unreleased\]\n", changelog, re.MULTILINE)
    if unreleased_match is None:
        raise SystemExit("CHANGELOG.md has no Unreleased section.")
    next_section = re.search(r"^## \[", changelog[unreleased_match.end() :], re.MULTILINE)
    if next_section is None:
        raise SystemExit("Could not find the next changelog section after Unreleased.")
    start = unreleased_match.end()
    end = start + next_section.start()
    unreleased_entries = changelog[start:end].strip()
    if not unreleased_entries:
        raise SystemExit("The Unreleased section is empty; add release notes before preparing.")

    new_changelog = (
        changelog[:start]
        + f"\n## [{args.version}] - {release_date.isoformat()}\n\n"
        + unreleased_entries
        + "\n\n"
        + changelog[end:]
    )
    new_module = module[: module_match.start(1)] + args.version + module[module_match.end(1) :]
    app_config["version"] = args.version
    new_app_config = json.dumps(app_config, indent=2, ensure_ascii=False) + "\n"

    CHANGELOG.write_text(new_changelog, encoding="utf-8")
    MODULE.write_text(new_module, encoding="utf-8")
    APP_CONFIG.write_text(new_app_config, encoding="utf-8")
    print(f"Prepared Meiro {args.version} dated {release_date.isoformat()}.")
    print("Review CHANGELOG.md, MODULE.bazel, and mygo.json before committing.")


if __name__ == "__main__":
    main()
