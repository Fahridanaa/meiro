# Working on Meiro

For MyGo (UI lifetime, element identity, threading, `ui.NewTester`), read
`.agents/skills/mygo-maintenance/SKILL.md` first. To prove a user-visible
change works, use `.agents/skills/verify-meiro/`.

## Checks

```sh
bazel test //...                 # every test
bazel test --config=race //...   # the same tests under the race detector, as CI runs them
bazel run //:lint
bazel run //:format-check        # bazel run //:format writes it
bazel run //:gazelle             # after adding, moving or deleting a Go file
```

A git `pre-push` hook (`.githooks/pre-push`) runs the format check and the
linters and blocks the push if either fails. Enable it once per clone with
`git config core.hooksPath .githooks`. Do not bypass it with `--no-verify`.

## Changelog

A change a user can see adds an entry under `## [Unreleased]` in
`CHANGELOG.md`, in the matching group (`Added`, `Changed`, `Fixed`,
`Removed`). Write what the user gets, not how the code works. Refactors,
tests and CI changes need no entry.

## Rules the linters cannot check

- The main thread owns `app`. Do not start goroutines outside `app.every`,
  `app.run` and the task helpers; hand results back with `startTask`,
  `spawn` or `a.run`.
- Tests drive the app like a user (`newTestApp()`, `ui.NewTester`) and never
  touch the network.
