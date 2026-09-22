# MCPaint

An MCP server that lets an AI create pictures and music with **Mario Paint**
(SNES, 1992) — the real game, running headless — and capture the result as PNG,
WAV and MP4.

## How it works

A libretro core runs the game with no window at all. Content is injected
straight into the console's WRAM at the addresses the game itself uses, and the
game renders it exactly as if it had been drawn by hand.

Drawing is played back step by step with the in-game cursor following the
stroke, so a recording shows Mario Paint *drawing the picture* rather than a
picture pasted into Mario Paint. Music is the SPC700's own output, captured
from the same run that produced the frames, so picture and sound need no
resynchronising.

## Requirements

- A Mario Paint ROM. **Not included** — supply your own copy.
- A libretro SNES core, e.g. `snes9x_libretro.dylib`.
- `ffmpeg` on PATH, for video and audio output.
- Go 1.24+.

## Running

```sh
export MCPAINT_CORE="$HOME/Library/Application Support/RetroArch/cores/snes9x_libretro.dylib"
export MCPAINT_ROM="/path/to/Mario Paint (JU).smc"
export MCPAINT_OUT="$HOME/mcpaint"   # optional, this is the default

go build -o build/mcpaint ./cmd/mcpaint
```

Register it with an MCP client:

```json
{
  "mcpServers": {
    "mcpaint": {
      "command": "/path/to/build/mcpaint",
      "env": {
        "MCPAINT_CORE": "/path/to/snes9x_libretro.dylib",
        "MCPAINT_ROM": "/path/to/Mario Paint (JU).smc"
      }
    }
  }
}
```

## Tools

| Tool | What it does |
|---|---|
| `reference` | The palette, instruments, note range and hard limits |
| `draw_image` | Redraw an existing image, quantized to 16 colours |
| `draw` | Draw with the game's own tools, optionally recording a timelapse |
| `screenshot` | Capture the canvas or the whole screen |
| `compose` | Write notes into the music composer |
| `import_midi` | Load a MIDI file, with a report of what had to give |
| `play` | Play the song and record audio, optionally with video |
| `record_session` | One clip: the picture being drawn, then the song playing |

## What the game allows

The limits are tight and shape everything:

- **Canvas** 248×164, 16 fixed colours, no blending.
- **Songs** up to 96 columns holding at most three notes each.
- **Pitches** are 13 diatonic staff positions, B3 to G5. There are no sharps
  or flats, so imported music gets snapped.

## Development CLI

```sh
# Draw an image onto the canvas and screenshot it
go run ./cmd/mcpaint-cli -image picture.png -out out/canvas.png

# Play a set of drawing operations and record the timelapse
go run ./cmd/mcpaint-cli -ops testdata/scene.json -video out/scene.mp4 -full

# Drive the MCP server end to end
go build -o build/mcpaint ./cmd/mcpaint && go run ./tools/mcptest
```

## Notes on the game

Mario Paint's title screen has no clickable "start": a full grid sweep found
only the letter gags. The attract demo walks into the canvas by itself, so the
boot rides it in and then clears the demo flag at `$7E:04E2` to take over. That
costs ~2400 emulated frames, so the resulting state is cached and reused.

Two things that look like they should work but do not:

- `$7E:0C24` is named "scroll end" and is not a song length. It sits at 784
  whatever is on the staff, and writing a column count there cuts playback off
  after the first note.
- Pressing PLAY twice in a row plays nothing the second time. The playhead
  stays at the end of the previous run, so STOP has to rewind it first.

Memory addresses come from the labelled RAM map in
[Yoshifanatic1/Mario-Paint-Disassembly](https://github.com/Yoshifanatic1/Mario-Paint-Disassembly)
and were each confirmed against the running game. The palette is decoded from
the game's own `Canvas.bin` rather than sampled from screenshots.
