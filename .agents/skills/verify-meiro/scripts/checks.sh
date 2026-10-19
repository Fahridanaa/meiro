#!/usr/bin/env bash
# Run the repository checks as one batch and print one concise summary.
set -uo pipefail
root=$(git rev-parse --show-toplevel)
cd "$root"
run_gazelle=0
if [ "${1:-}" = --gazelle ]; then
	run_gazelle=1
	shift
fi
[ "$#" -eq 0 ] || { echo "usage: checks.sh [--gazelle]" >&2; exit 2; }

logs=$(mktemp -d)
trap 'rm -rf "$logs"' EXIT
names=()
statuses=()

run() {
	local name=$1
	shift
	local log="$logs/${#names[@]}.log"
	printf 'RUN   %s\n' "$name"
	if "$@" >"$log" 2>&1; then
		names+=("$name")
		statuses+=(PASS)
	else
		local status=$?
		names+=("$name")
		statuses+=("FAIL($status)")
		cp "$log" "$logs/failure-${#names[@]}.log"
	fi
}

if command -v bazel >/dev/null 2>&1; then
	run 'bazel test //...' bazel test //...
	run 'bazel test --config=race //...' bazel test --config=race //...
	run 'bazel run //:lint' bazel run //:lint
	run 'bazel run //:format-check' bazel run //:format-check
	if [ "$run_gazelle" -eq 1 ]; then run 'bazel run //:gazelle' bazel run //:gazelle; fi
else
	echo 'NOTE  bazel unavailable; using Go test and vet, lint/format skipped'
	run 'go test ./... -count=1' go test ./... -count=1
	run 'go vet ./...' go vet ./...
fi

printf '\nCHECK SUMMARY\n'
failed=0
for i in "${!names[@]}"; do
	printf '%-38s %s\n' "${names[$i]}" "${statuses[$i]}"
	[[ "${statuses[$i]}" == PASS ]] || failed=1
done
if [ "$failed" -ne 0 ]; then
	for failure in "$logs"/failure-*.log; do
		[ -e "$failure" ] || continue
		printf '\n--- %s (last 60 lines) ---\n' "$(basename "$failure")"
		tail -n 60 "$failure"
	done
fi
exit "$failed"
