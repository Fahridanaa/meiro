#!/usr/bin/env bash
# Bootstrap and check this checkout for headless and native proofs. Exit 1 on a blocker.
set -u
cd "$(git rev-parse --show-toplevel)" || exit 1
fail=0
ok() { printf 'ok    %s\n' "$1"; }
bad() { printf 'FAIL  %s\n' "$1"; fail=1; }
note() { printf 'note  %s\n' "$1"; }

want=$(awk '/^go /{print $2}' go.mod)
have=$(go env GOVERSION 2>/dev/null | sed 's/^go//')
if [ -n "$have" ] && [ "$(printf '%s\n%s\n' "$want" "$have" | sort -V | head -1)" = "$want" ]; then
	ok "go $have (go.mod wants $want)"
else
	bad "go ${have:-missing}, go.mod wants $want"
fi

mygo=$(go list -m -f '{{.Dir}}' github.com/egoist/mygo 2>/dev/null)
if [ -z "$mygo" ] || [ ! -d "$mygo" ]; then
	if go mod download github.com/egoist/mygo >/dev/null 2>&1; then
		mygo=$(go list -m -f '{{.Dir}}' github.com/egoist/mygo 2>/dev/null)
	fi
fi
if [ -n "$mygo" ] && [ -d "$mygo" ]; then ok "mygo source at $mygo"; else bad "could not download mygo into the module cache"; fi

if [ "$(git rev-parse --is-shallow-repository 2>/dev/null)" = true ]; then
	if git fetch --quiet --unshallow origin; then
		ok "fetched complete git history from origin"
	else
		bad "could not unshallow this checkout from origin"
	fi
fi

if command -v ffmpeg >/dev/null; then ok "ffmpeg $(ffmpeg -version | head -1 | cut -d' ' -f3)"; else bad "ffmpeg missing: playback proofs need it"; fi
command -v yt-dlp >/dev/null && ok "yt-dlp present" || note "yt-dlp missing: fine, tests replace the resolver"
command -v bazel >/dev/null && ok "bazel present (lint, format, race)" || note "bazel missing: use go test, skip lint and format"

if command -v amp >/dev/null 2>&1; then
	ensure_log=$(mktemp)
	if amp orb desktop ensure >"$ensure_log" 2>&1; then
		ok "Desktop ensure completed"
	else
		note "Desktop ensure failed; native availability is not established"
		tail -n 3 "$ensure_log" | sed 's/^/      /'
	fi
	rm -f "$ensure_log"
else
	note "amp CLI unavailable; Desktop state is unknown (native check skipped)"
fi

if [ -f "$HOME/.cache/amp/desktop/environment" ]; then
	# shellcheck disable=SC1091
	. "$HOME/.cache/amp/desktop/environment"
	if [ -n "${XDG_RUNTIME_DIR:-}" ] && [ -n "${WAYLAND_DISPLAY:-}" ] &&
		[ -S "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY" ] && command -v grim >/dev/null &&
		grim - >/dev/null 2>&1; then
		ok "Desktop is up: the native window can open (scripts/native.sh)"
		for t in grim wtype wlrctl yt-dlp; do
			command -v "$t" >/dev/null && ok "$t" || bad "$t missing: run .agents/setup"
		done
	else
		note "Desktop was ensured but is not currently usable; retry 'amp orb desktop ensure' before scripts/native.sh"
	fi
else
	note "Desktop environment was not created; native availability is unknown, headless proofs still work"
fi

if [ -n "$(git status --porcelain -- . ':!.gitignore')" ]; then
	note "working tree has changes: this is the 'after' state"
fi
ls zz_proof*_test.go >/dev/null 2>&1 && note "scratch proofs present: $(ls zz_proof*_test.go | tr '\n' ' ')delete them before committing"

if go vet ./youtube/ >/dev/null 2>&1; then ok "go vet ./youtube builds"; else bad "go vet ./youtube failed: the module does not compile"; fi
exit $fail
