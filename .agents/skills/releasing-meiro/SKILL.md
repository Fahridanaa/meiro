---
name: releasing-meiro
description: Prepares and publishes Meiro desktop releases and checks out/reviews Meiro pull requests, including changelogs, version tags, GitHub releases, CI monitoring, and GitHub comments. Use when preparing or shipping a release or reviewing a Meiro PR.
---

# Releasing Meiro

Prepare and publish a Meiro release using the repository's version tags and GitHub Actions workflows.

## Prepare the release

1. Confirm the working tree has no unrelated changes. Read `CHANGELOG.md`,
   `mygo.json`, `MODULE.bazel`, and `.github/workflows/release.yml`.
2. Review every commit since the previous `desktop-vX.Y.Z` tag. Add concise,
   user-facing notes for shipped changes under `## [Unreleased]` in
   `CHANGELOG.md`; do not list refactors or CI-only changes.
3. Test the release-preparation helper against its isolated fixture before
   using it on repository files. Python bytecode is disabled so the skill tree
   stays clean:

   ```sh
   PYTHONDONTWRITEBYTECODE=1 python3 -B .agents/skills/releasing-meiro/scripts/test_prepare_release.py
   ```

4. Dry-run on the repository and inspect the printed diff. The helper is
   read-only by default; pass the version without `v`. Use `--date YYYY-MM-DD`
   only when needed:

   ```sh
   PYTHONDONTWRITEBYTECODE=1 python3 -B .agents/skills/releasing-meiro/scripts/prepare_release.py 0.4.2
   ```

   Only after reviewing the diff, run it again with `--apply`. The helper
   validates all inputs before writing atomically, then verifies the changelog
   section and matching versions in `MODULE.bazel` and `mygo.json`. You can
   repeat that integrity check at any time with `--verify`:

   ```sh
   PYTHONDONTWRITEBYTECODE=1 python3 -B .agents/skills/releasing-meiro/scripts/prepare_release.py 0.4.2 --apply
   PYTHONDONTWRITEBYTECODE=1 python3 -B .agents/skills/releasing-meiro/scripts/prepare_release.py 0.4.2 --verify
   ```

5. Run the repository release checks in one batch; this prints a final
   pass/fail summary and failure details without dumping each long command's
   progress into the transcript:

   ```sh
   .agents/skills/verify-meiro/scripts/checks.sh
   ```

## Publish and monitor

1. Commit only the release version, changelog, and preparation tooling. Do not
   include unrelated changes.
2. Before pushing that commit, update against the remote and rebase; do not
   respond to a rejected push by force-pushing:

   ```sh
   git fetch origin master
   git rebase origin/master
   ```

   If `CHANGELOG.md` conflicts in `## [Unreleased]`, combine both sides by
   category, keep upstream bullets first and local bullets second, and remove
   only exact duplicate bullets. For a dated release section, keep upstream's
   published history authoritative and move any genuinely new notes to
   Unreleased. If release versions or section ordering conflict, stop and
   resolve the release intent before continuing; never choose ours/theirs for
   the entire file. Rerun checks after resolving conflicts, then push the
   rebased commit to `master`.
3. Wait for `CI` on the exact release commit. List runs for its SHA using only
   supported `gh` JSON fields, then wait with the bundled quiet wrapper:

   ```sh
   gh run list --commit "$sha" --workflow CI --json databaseId,headSha,status,conclusion,url
   .agents/skills/releasing-meiro/scripts/watch_run.sh RUN_ID "$sha"
   ```

   `watch_run.sh` polls silently with supported `gh run view` fields and prints
   one final summary; unlike `gh run watch`, it has no 600-second wait limit.
   It refuses a run whose `headSha` differs from the expected full commit SHA.
   If any job fails, fix the cause, commit and push after the fetch/rebase
   step, and wait for that exact commit's CI before tagging.
4. Create and push the full-build release tag only after CI passes:

   ```sh
   git tag desktop-v0.4.0
   git push origin desktop-v0.4.0
   ```

   The tag push starts `.github/workflows/release.yml`. It checks that the tag
   matches `mygo.json`, waits for CI on that commit, builds platform packages,
   and publishes the full release plus the minimal-update release. The
   workflow takes release notes from the version's `CHANGELOG.md` section.
5. Watch the `Release` workflow with `watch_run.sh`. Investigate failures
   before retrying; a manual workflow run on the same tag is the documented
   retry path. Confirm both releases, assets, and GitHub's actual latest tag:

   ```sh
   .agents/skills/releasing-meiro/scripts/verify_release.sh 0.4.2
   ```

   The script uses valid `gh release view` fields (`tagName`, `isDraft`,
   `isPrerelease`, `assets`, `url`) and the Releases API's `/latest` endpoint;
   there is no `isLatest` field in `gh release view`. It confirms the two
   Meiro releases are not GitHub's latest because the workflow publishes each
   with `--latest=false`.

6. Do not mark a release as GitHub's latest manually. Meiro's updater uses
   the `desktop-v` tag prefix, and both release workflows publish with
   `--latest=false` by design.

Do not force-push or move an existing release tag. If the tag already exists,
inspect its commit and release state before taking any action.

## Pull requests and GitHub comments

When reviewing a PR, check it out in a separate worktree so tests run on the
actual PR contents rather than using CI as a proxy:

```sh
gh pr checkout 166 --worktree ../meiro-pr-166
cd ../meiro-pr-166
```

Run the relevant checks from that worktree. Remove it with `git worktree
remove ../meiro-pr-166` when finished; do not disturb the primary checkout.

Use the `gh` comment commands for ordinary PR/issue follow-up instead of
inventing API calls or searching for absent MCP write tools:

```sh
gh pr comment 166 --body-file comment.md
gh issue comment 123 --body-file comment.md
gh pr review 166 --comment --body-file review.md
```

Only post comments or reviews when the user explicitly asks. Check command
JSON fields against `gh <command> --help`; for example, `gh run view` supports
`jobs`, but `gh run list` does not, and `gh release view` has no `isLatest`.
