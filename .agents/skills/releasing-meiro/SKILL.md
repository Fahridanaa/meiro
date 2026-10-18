---
name: releasing-meiro
description: Prepares and publishes Meiro desktop releases, including changelog updates, version tags, GitHub releases, and CI monitoring. Use when preparing or shipping a Meiro release.
---

# Releasing Meiro

Prepare and publish a Meiro release using the repository's version tags and GitHub Actions workflows.

## Prepare the release

1. Confirm the working tree has no unrelated changes. Read `CHANGELOG.md`,
   `mygo.json`, `MODULE.bazel`, and `.github/workflows/release.yml`.
2. Review every commit since the previous `desktop-vX.Y.Z` tag. Add concise,
   user-facing notes for shipped changes under `## [Unreleased]` in
   `CHANGELOG.md`; do not list refactors or CI-only changes.
3. Run the preparation helper from the repository root:

   ```sh
   python3 .agents/skills/releasing-meiro/scripts/prepare_release.py 0.4.0
   ```

   Pass the version without a `v`. The helper moves the Unreleased entries
   into a dated version section and updates both `mygo.json` and
   `MODULE.bazel`. Use `--date YYYY-MM-DD` only when the release date differs
   from the local date. Review the resulting diff before continuing.
4. Run the repository release checks from `AGENTS.md`:

   ```sh
   bazel test //...
   bazel test --config=race //...
   bazel run //:lint
   bazel run //:format-check
   ```

## Publish and monitor

1. Commit the version, changelog, and release-preparation tooling with a
   message such as `Prepare 0.4.0 release`, then push the release commit to
   `master`. Do not include unrelated changes.
2. Wait for the `CI` workflow to complete successfully on the exact release
   commit. If any job fails, fix the cause, push a new commit, and wait for
   that commit's CI before tagging.
3. Create and push the full-build release tag only after CI passes:

   ```sh
   git tag desktop-v0.4.0
   git push origin desktop-v0.4.0
   ```

   The tag push starts `.github/workflows/release.yml`. It checks that the tag
   matches `mygo.json`, waits for CI on that commit, builds platform packages,
   and publishes the full release plus the minimal-update release. The
   workflow takes release notes from the version's `CHANGELOG.md` section.
4. Watch the `Release` workflow until it completes. Investigate failures
   before retrying; a manual workflow run on the same tag is the documented
   retry path. Confirm both `desktop-vX.Y.Z` and
   `desktop-minimal-vX.Y.Z` releases and their assets are present.
5. Do not mark the release as GitHub's latest release manually. Meiro's
   updater uses the `desktop-v` tag prefix, and the workflow publishes with
   `--latest=false` by design.

Do not force-push or move an existing release tag. If the tag already exists,
inspect its commit and release state before taking any action.
