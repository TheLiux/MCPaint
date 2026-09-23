#!/bin/sh
# Rebuilds every example from its source. Needs MCPAINT_CORE and
# MCPAINT_ROM set, and ffmpeg on PATH. Run from the repository root:
#
#   sh examples/make.sh
set -eu

: "${MCPAINT_CORE:?set MCPAINT_CORE to a libretro SNES core}"
: "${MCPAINT_ROM:?set MCPAINT_ROM to a Mario Paint ROM}"

go build -o build/ ./cmd/...
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# gif turns a recording into a small looping GIF at the canvas's own size.
gif() {
	ffmpeg -loglevel error -y -i "$1" -vf \
		"fps=15,scale=iw/3:-1:flags=neighbor,split[a][b];[a]palettegen=max_colors=32[p];[b][p]paletteuse=dither=none" \
		"$2"
}

# A photograph-like picture: dithered, matched on lightness.
build/mcpaint-cli -image examples/fractal/source.png -fit cover \
	-strokes -seconds 10 -music off \
	-video "$tmp/fractal.mp4" -out examples/fractal/canvas.png
gif "$tmp/fractal.mp4" examples/fractal/drawing.gif

# Flat pixel art: no dithering, hue first, so every block stays one colour.
build/mcpaint-cli -image examples/robot/source.png -fit contain \
	-dither=false -vivid -strokes -seconds 6 -music off \
	-video "$tmp/robot.mp4" -out examples/robot/canvas.png
gif "$tmp/robot.mp4" examples/robot/drawing.gif

# Drawing operations: shapes laid down with the game's own tools.
build/mcpaint-cli -ops testdata/scene.json -out examples/scene.png

# A MIDI file in D major, two parts, longer than one staff.
build/mcpaint-midi -midi examples/ode/ode.mid -auto-key -steps 2 -tempo 17 \
	-instruments "0=star,1=mario" \
	-out "$tmp/ode.mp4" -wav "$tmp/ode.wav"
cp "$tmp/staff.png" examples/ode/staff.png
ffmpeg -loglevel error -y -i "$tmp/ode.wav" -b:a 96k examples/ode/ode.mp3
