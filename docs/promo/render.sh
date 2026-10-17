#!/bin/sh
# Render hero-light.html and hero-dark.html to 2400x1500 PNGs beside them.
cd "$(dirname "$0")" || exit 1
agent-browser set viewport 1600 1000 2 >/dev/null
for t in light dark; do
  agent-browser open "file://$PWD/hero-$t.html" >/dev/null && sleep 1 &&
    agent-browser screenshot "$PWD/hero-$t.png" >/dev/null &&
    magick "$PWD/hero-$t.png" -resize 2400x "$PWD/hero-$t.png"
done
