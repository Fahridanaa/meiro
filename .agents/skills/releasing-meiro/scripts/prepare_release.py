#!/usr/bin/env python3
"""Move the Unreleased changelog section into a dated Meiro release."""

from __future__ import annotations

import argparse
import difflib
import json
import re
import stat
from datetime import date
from pathlib import Path
from tempfile import NamedTemporaryFile


ROOT = Path(__file__).resolve().parents[4]
VERSION_PATTERN = re.compile(r"^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)$")


def parse_version(value: str) -> tuple[int, int, int]:
    match = VERSION_PATTERN.fullmatch(value)
    if match is None:
        raise ValueError(f"Version must be numeric x.y.z without a prefix: {value}")
    return tuple(map(int, match.groups()))


def prepare(root: Path, version: str, release_date: date) -> dict[Path, str]:
    target = parse_version(version)
    changelog_path = root / "CHANGELOG.md"
    module_path = root / "MODULE.bazel"
    app_config_path = root / "mygo.json"
    changelog = changelog_path.read_text(encoding="utf-8")
    module = module_path.read_text(encoding="utf-8")
    app_config = json.loads(app_config_path.read_text(encoding="utf-8"))

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
        raise SystemExit(f"New version {version} must be greater than {current_versions[0]}.")

    release_header = f"## [{version}]"
    if re.search(rf"^{re.escape(release_header)}(?:\s|$)", changelog, re.MULTILINE):
        raise SystemExit(f"CHANGELOG.md already has a section for {version}.")
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
        + f"\n## [{version}] - {release_date.isoformat()}\n\n"
        + unreleased_entries
        + "\n\n"
        + changelog[end:]
    )
    new_module = module[: module_match.start(1)] + version + module[module_match.end(1) :]
    app_config["version"] = version
    new_app_config = json.dumps(app_config, indent=2, ensure_ascii=False) + "\n"
    return {
        changelog_path: new_changelog,
        module_path: new_module,
        app_config_path: new_app_config,
    }


def verify(root: Path, version: str) -> None:
    parse_version(version)
    module = (root / "MODULE.bazel").read_text(encoding="utf-8")
    app_config = json.loads((root / "mygo.json").read_text(encoding="utf-8"))
    module_match = re.search(r'^    version = "([^"]+)",$', module, re.MULTILINE)
    if module_match is None:
        raise SystemExit("Could not find the version in MODULE.bazel.")
    if module_match.group(1) != version or app_config.get("version") != version:
        raise SystemExit(
            f"Version mismatch: expected {version}, MODULE.bazel has "
            f"{module_match.group(1)}, mygo.json has {app_config.get('version')}."
        )
    changelog = (root / "CHANGELOG.md").read_text(encoding="utf-8")
    match = re.search(
        rf"^## \[{re.escape(version)}\] - (\d{{4}}-\d{{2}}-\d{{2}})\n",
        changelog,
        re.MULTILINE,
    )
    if match is None:
        raise SystemExit(f"CHANGELOG.md has no dated section for {version}.")
    try:
        date.fromisoformat(match.group(1))
    except ValueError as error:
        raise SystemExit(f"CHANGELOG.md has an invalid release date for {version}.") from error
    next_section = re.search(r"^## \[", changelog[match.end() :], re.MULTILINE)
    section_end = match.end() + next_section.start() if next_section else len(changelog)
    if not changelog[match.end() : section_end].strip():
        raise SystemExit(f"CHANGELOG.md section for {version} is empty.")


def atomic_write(path: Path, contents: str) -> None:
    mode = stat.S_IMODE(path.stat().st_mode)
    temporary_path: Path | None = None
    try:
        with NamedTemporaryFile("w", encoding="utf-8", dir=path.parent, delete=False) as temp:
            temp.write(contents)
            temporary_path = Path(temp.name)
        temporary_path.chmod(mode)
        temporary_path.replace(path)
    finally:
        if temporary_path is not None:
            temporary_path.unlink(missing_ok=True)


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("version", help="release version without a v prefix")
    parser.add_argument("--date", default=date.today().isoformat(), help="release date (YYYY-MM-DD)")
    parser.add_argument(
        "--root", type=Path, default=ROOT, help="repository or fixture root (default: current repo)"
    )
    parser.add_argument("--apply", action="store_true", help="write the prepared files; default only prints a diff")
    parser.add_argument("--verify", action="store_true", help="verify an already prepared release without writing")
    args = parser.parse_args()

    try:
        parse_version(args.version)
    except ValueError as error:
        parser.error(str(error))
    if args.apply and args.verify:
        parser.error("--apply and --verify cannot be used together")
    if re.fullmatch(r"\d{4}-\d{2}-\d{2}", args.date) is None:
        parser.error(f"Invalid release date: {args.date}; expected YYYY-MM-DD.")
    try:
        release_date = date.fromisoformat(args.date)
    except ValueError as error:
        parser.error(f"Invalid release date: {args.date}.")

    root = args.root.resolve()
    if args.verify:
        verify(root, args.version)
        print(f"Verified Meiro {args.version}: changelog, MODULE.bazel, and mygo.json agree.")
        return

    changes = prepare(root, args.version, release_date)
    if not args.apply:
        for path, contents in changes.items():
            previous = path.read_text(encoding="utf-8").splitlines(keepends=True)
            proposed = contents.splitlines(keepends=True)
            diff = difflib.unified_diff(previous, proposed, fromfile=str(path), tofile=f"{path} (prepared)")
            print("".join(diff), end="")
        print("Dry run only; pass --apply to write these three files.")
        return

    for path, contents in changes.items():
        atomic_write(path, contents)
    verify(root, args.version)
    print(f"Prepared and verified Meiro {args.version} dated {release_date.isoformat()}.")
    print("Review CHANGELOG.md, MODULE.bazel, and mygo.json before committing.")


if __name__ == "__main__":
    main()
