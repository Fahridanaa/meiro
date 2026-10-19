#!/usr/bin/env bash
# Runs scratch proof tests and keeps their screenshots, step texts and log.
#
#   prove.sh [--before REF] [--label NAME] [--race] TEST_REGEX
#
# After: runs the tests on the working tree. Before: runs the same scratch
# test files on a clean worktree of REF (use "base" for the merge base with
# master). The before run is expected to fail when it proves a fix or a
# feature; its exit code is recorded, not enforced.
# Artifacts: .amp/in/artifacts/verify-meiro/<label>/{after,before}/
set -euo pipefail
root=$(git rev-parse --show-toplevel)
cd "$root"
mkdir -p "$root/.amp/in"

before="" label="" race=()
while [ $# -gt 0 ]; do
	case $1 in
	--before) before=$2; shift 2 ;;
	--label) label=$2; shift 2 ;;
	--race) race=(-race); shift ;;
	*) break ;;
	esac
done
regex=${1:?usage: prove.sh [--before REF] [--label NAME] [--race] TEST_REGEX}
label=${label:-$(date +%Y%m%d-%H%M%S)}
out="$root/.amp/in/artifacts/verify-meiro/$label"
mkdir -p "$(dirname "$(git rev-parse --git-path info/exclude)")"
grep -qxF '.amp/in/' "$(git rev-parse --git-path info/exclude)" || echo '.amp/in/' >>"$(git rev-parse --git-path info/exclude)"
mkdir -p "$out"

shopt -s nullglob
scratch=(zz_proof*_test.go)
[ ${#scratch[@]} -gt 0 ] || { echo "no zz_proof*_test.go: copy templates/zz_proof_test.go.tmpl first" >&2; exit 2; }

support_template="$(dirname "$0")/../templates/zz_proof_helpers_test.go.tmpl"
support="$root/zz_proof_support_test.go"
needs_support=1
if grep -q 'func newProof(' "${scratch[@]}"; then needs_support=0; fi
[ ! -e "$support" ] || { echo "$support already exists; preserve it and rerun after resolving the collision" >&2; exit 2; }
if [ "$needs_support" -eq 1 ]; then cp "$support_template" "$support"; fi
wt=""
cleanup() {
	[ -z "$wt" ] || git worktree remove --force "$wt" >/dev/null 2>&1 || true
	[ "$needs_support" -eq 0 ] || rm -f "$support"
}
trap cleanup EXIT

run() { # dir name
	local dir=$1 name=$2 code=0
	rm -rf "$out/$name"; mkdir -p "$out/$name"
	(cd "$dir" && PROOF_DIR="$out/$name" go test -count=1 -v "${race[@]}" -run "$regex" . ) >"$out/$name/go-test.log" 2>&1 || code=$?
	echo "$code" >"$out/$name/exit-code"
	grep -v '^ALSA lib' "$out/$name/go-test.log" | grep -E '^(---|===|ok|FAIL|PASS|panic)|_test.go:' | head -40
	echo "[$name] exit $code, $(ls "$out/$name"/*.png 2>/dev/null | wc -l) screenshots in $out/$name"
	return 0
}

if [ -n "$before" ]; then
	if [ "$(git rev-parse --is-shallow-repository)" = true ]; then
		git fetch --quiet --unshallow origin
	fi
	if [ "$before" = base ]; then
		before=$(git merge-base HEAD master)
	fi
	wt=$(mktemp -d "$root/.amp/in/worktree.XXXXXX")
	git worktree add --detach "$wt" "$before" >/dev/null
	cp "${scratch[@]}" "$wt"/
	if [ "$needs_support" -eq 1 ]; then cp "$support_template" "$wt/zz_proof_support_test.go"; fi
	echo "before = $(git rev-parse --short "$before")"
	run "$wt" before
fi

echo "after = working tree on $(git rev-parse --short HEAD)"
run "$root" after
exit "$(cat "$out/after/exit-code")"
