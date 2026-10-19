#!/usr/bin/env bash
# Verify both published Meiro releases and report GitHub's actual latest tag.
set -euo pipefail
version=${1:?usage: verify_release.sh VERSION}
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || {
	echo "version must be numeric x.y.z without a v prefix" >&2
	exit 2
}
repo=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
full="desktop-v$version"
minimal="desktop-minimal-v$version"

for tag in "$full" "$minimal"; do
	release=$(gh release view "$tag" --json tagName,isDraft,isPrerelease,assets,url)
	echo "$release" | jq -e --arg tag "$tag" '.tagName == $tag and (.isDraft | not) and (.isPrerelease | not) and (.assets | length > 0)' >/dev/null
	echo "$release" | jq -r '"published \(.tagName): \(.assets | length) assets\n\(.url)"'
done

if ! latest=$(gh api "repos/$repo/releases/latest" --jq .tag_name); then
	echo "could not confirm GitHub's latest release through the Releases API" >&2
	exit 1
fi
printf 'GitHub latest release: %s\n' "$latest"
if [ "$latest" = "$full" ] || [ "$latest" = "$minimal" ]; then
	echo "unexpected: this workflow publishes both releases with --latest=false" >&2
	exit 1
fi
echo "confirmed: $full and $minimal are not GitHub's latest release"
