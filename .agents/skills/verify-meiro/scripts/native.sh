#!/usr/bin/env bash
# Runs the real Meiro window on the orb Desktop and drives it.
#
#   native.sh start            build, start the Desktop if needed, launch Meiro on empty data
#   native.sh shot NAME        screenshot of the whole Desktop to <out>/NAME.png
#   native.sh capture-loop [SECONDS] [DURATION] [NAME] timed screenshot fallback
#   native.sh click X Y        left click at Desktop pixel X,Y of the last screenshot
#   native.sh rclick X Y       right click
#   native.sh type TEXT        type text into the focused field
#   native.sh key KEY...       press keys, "ctrl+k", "Return", "Tab", "space"
#   native.sh status           running pid, or "stopped"
#   native.sh stop             stop the instance this script started
#
# Screenshots go to .amp/in/artifacts/verify-meiro/native/ (NATIVE_OUT overrides).
# Data lives in .amp/in/native/data, wiped by every start. It reaches real
# YouTube Music, signed out. Never put a real cookie in it.
set -euo pipefail
root=$(git rev-parse --show-toplevel)
state="$root/.amp/in/native"
bin="$root/.amp/in/bin/meiro"
out=${NATIVE_OUT:-$root/.amp/in/artifacts/verify-meiro/native}
envfile="$HOME/.cache/amp/desktop/environment"

desktop_env() {
	[ -f "$envfile" ] || { echo "no Desktop: run 'amp orb desktop ensure'" >&2; exit 1; }
	# shellcheck disable=SC1090
	. "$envfile"
}
pid() { [ -f "$state/pid" ] && kill -0 "$(cat "$state/pid")" 2>/dev/null && cat "$state/pid"; }

cmd=${1:-}
shift || true
case $cmd in
start)
	amp orb desktop ensure >/dev/null
	desktop_env
	mkdir -p "$state" "$out" "$(dirname "$bin")"
	(cd "$root" && go build -o "$bin" .)
	if p=$(pid); then kill "$p"; sleep 1; fi
	rm -rf "$state/data" && mkdir -p "$state/data"/{config,cache,share,state}
	rm -f "$state/pid"
	/opt/amp-desktop/launch-app env \
		PATH="$HOME/.local/bin:$PATH" \
		XDG_CONFIG_HOME="$state/data/config" XDG_CACHE_HOME="$state/data/cache" \
		XDG_DATA_HOME="$state/data/share" XDG_STATE_HOME="$state/data/state" \
		"$bin"
	for _ in $(seq 1 100); do
		p=$(pgrep -fx "$bin" | head -1 || true)
		[ -n "$p" ] && break
		sleep 0.1
	done
	[ -n "${p:-}" ] || { echo "Meiro did not start: see ~/.cache/amp/desktop/custom-app.log" >&2; exit 1; }
	echo "$p" >"$state/pid"
	for _ in $(seq 1 100); do # ready when the window is mapped and has drawn
		if grim - 2>/dev/null | wc -c | awk '{exit !($1>100000)}' && wlr-randr >/dev/null 2>&1; then break; fi
		sleep 0.2
	done
	# Never change the output mode or scale: it crashes the Desktop tab's
	# stream. Chrome fills the output, so fullscreen Meiro to put its
	# top-left corner at 0,0.
	wlrctl window focus title:Meiro
	wlrctl window fullscreen title:Meiro
	sleep 3
	echo "meiro pid $p, data $state/data"
	;;
shot)
	desktop_env
	mkdir -p "$out"
	grim "$out/${1:?name}.png"
	echo "$out/$1.png"
	;;
click | rclick)
	desktop_env
	button=left
	[ "$cmd" = rclick ] && button=right
	# The pointer takes logical pixels, a screenshot has physical ones.
	scale=$(wlr-randr | awk '/Scale:/{print $2; exit}')
	x=$(awk -v v="${1:?x}" -v s="$scale" 'BEGIN{printf "%d", v/s}')
	y=$(awk -v v="${2:?y}" -v s="$scale" 'BEGIN{printf "%d", v/s}')
	wlrctl pointer move -5000 -5000
	wlrctl pointer move "$x" "$y"
	wlrctl pointer click "$button"
	sleep 0.7
	;;
type)
	desktop_env
	wtype -- "${1:?text}"
	sleep 0.5
	;;
key)
	desktop_env
	for spec in "$@"; do
		mods=() key=${spec##*+}
		if [ "$key" != "$spec" ]; then
			IFS=+ read -ra parts <<<"${spec%+*}"
			for m in "${parts[@]}"; do mods+=(-M "$m"); done
		fi
		wtype "${mods[@]}" -k "$key"
		sleep 0.5
	done
	;;
status)
	pid || echo stopped
	;;
stop)
	if p=$(pid); then kill "$p"; rm -f "$state/pid"; echo "stopped $p"; else echo "not running"; fi
	;;
capture-loop)
	desktop_env
	interval=${1:-1}
	duration=${2:-10}
	name=${3:-capture-$(date +%Y%m%d-%H%M%S)}
	if ! awk -v n="$interval" -v d="$duration" 'BEGIN { exit !(n+0>0 && d+0>0) }'; then
		echo "interval and duration must be positive numbers" >&2
		exit 2
	fi
	[[ "$name" =~ ^[A-Za-z0-9._-]+$ && "$name" != . && "$name" != .. ]] || {
		echo "capture name may contain only letters, numbers, dot, underscore and dash" >&2
		exit 2
	}
	frames="$out/$name"
	[ ! -e "$frames" ] || { echo "capture directory already exists: $frames" >&2; exit 2; }
	mkdir -p "$frames"
	count=$(awk -v d="$duration" -v n="$interval" 'BEGIN { print int(d/n) + 1 }')
	for ((i = 1; i <= count; i++)); do
		file=$(printf '%s/%04d.png' "$frames" "$i")
		grim "$file"
		printf '%s\t%s\n' "$i" "$(date --iso-8601=ns)" >>"$frames/timestamps.tsv"
		[ "$i" -eq "$count" ] || sleep "$interval"
	done
	echo "captured $count native screenshots in $frames (no video encoding)"
	;;
*)
	sed -n 2,16p "$0" | sed 's/^# \{0,1\}//'
	exit 2
	;;
esac
