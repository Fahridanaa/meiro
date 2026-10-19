#!/usr/bin/env bash
# Wait indefinitely for one GitHub Actions run and print its final state once.
set -uo pipefail
run_id=${1:?usage: watch_run.sh RUN_ID COMMIT_SHA}
expected_sha=${2:?usage: watch_run.sh RUN_ID COMMIT_SHA}
[[ "$expected_sha" =~ ^[[:xdigit:]]{40}$ ]] || { echo "expected a full 40-character commit SHA" >&2; exit 2; }
command -v gh >/dev/null 2>&1 || { echo "gh CLI is required" >&2; exit 2; }

while :; do
	result=$(gh run view "$run_id" --json name,status,conclusion,headSha,url) || {
		echo "could not read GitHub Actions run $run_id" >&2
		exit 2
	}
	actual_sha=$(echo "$result" | jq -r '.headSha')
	if [ "$actual_sha" != "$expected_sha" ]; then
		echo "run $run_id is for $actual_sha, not expected commit $expected_sha" >&2
		exit 2
	fi
	[ "$(echo "$result" | jq -r '.status')" = completed ] && break
	sleep 15
done

echo "$result" | jq -r '"\(.name): \(.status) / \(.conclusion // "pending")\ncommit: \(.headSha)\n\(.url)"'

if [ "$(echo "$result" | jq -r '.conclusion')" != success ]; then
	exit 1
fi
