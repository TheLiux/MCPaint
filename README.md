# MCPaint

An MCP server that lets an AI create pictures and music with **Mario Paint**
(SNES, 1992) — the real game, running headless — and capture the result as PNG,
WAV and MP4.

## How it works

A libretro core runs the game with no window at all. Content is injected
straight into the console's WRAM at the addresses the game itself uses, and the
game renders it exactly as if it had been drawn by hand. Drawing is played back
step by step with the in-game cursor following the stroke, so the recording
shows Mario Paint *drawing the picture*, not a picture pasted into Mario Paint.

## Requirements

- A Mario Paint ROM. **Not included** — supply your own copy.
- A libretro SNES core, e.g. `snes9x_libretro.dylib`.
- `ffmpeg` on PATH, for video and audio output.
- Go 1.24+.

## Development CLI

```sh
export MCPAINT_CORE="$HOME/Library/Application Support/RetroArch/cores/snes9x_libretro.dylib"
export MCPAINT_ROM="/path/to/Mario Paint (JU).smc"

# Draw an image onto the canvas and screenshot it
go run ./cmd/mcpaint-cli -image picture.png -out out/canvas.png

# Play a set of drawing operations and record the timelapse
go run ./cmd/mcpaint-cli -ops testdata/scene.json -video out/scene.mp4 -full
```

## Notes on the game

Mario Paint's title screen has no clickable "start": every hotspot up there is
one of the letter gags. The attract demo walks into the canvas by itself, so
the boot rides it in and then clears the demo flag at `$7E:04E2` to take over.
That costs ~2400 emulated frames, so the resulting state is cached and reused.

Memory addresses come from the labelled RAM map in
[Yoshifanatic1/Mario-Paint-Disassembly](https://github.com/Yoshifanatic1/Mario-Paint-Disassembly),
verified against the running game.
