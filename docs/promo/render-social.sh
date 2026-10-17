#!/bin/sh
# Render social-light.html and social-dark.html to 1280x640 PNGs, the size GitHub
# recommends for a repository social preview.
cd "$(dirname "$0")" || exit 1
agent-browser set viewport 1280 640 2 >/dev/null
for t in light dark; do
  agent-browser open "file://$PWD/social-$t.html" >/dev/null && sleep 1 &&
    agent-browser screenshot "$PWD/social-$t.png" >/dev/null &&
    magick "$PWD/social-$t.png" -resize 1280x640 "$PWD/social-$t.png"
done
