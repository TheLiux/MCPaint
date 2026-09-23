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
picture pasted into Mario Paint. It comes with sound: the canvas background
track plays over the timelapse, and a recording opens on the tune's first
note.

All audio is the SPC700's own output, captured from the same run that produced
the frames, so picture and sound need no resynchronising.

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
| `draw` | Draw with the game's own tools, optionally recording a timelapse with sound |
| `screenshot` | Capture the canvas or the whole screen |
| `compose` | Write notes into the music composer |
| `import_midi` | Load a MIDI file, with a report of what had to give |
| `play` | Play the song and record audio, optionally with video |
| `record_session` | One clip: the picture being drawn, then the song playing |

## Colour

Sixteen fixed colours with no blending, so how a source image is matched to
them matters more than usual.

- Matching is done in **Oklab**. Luminance-weighted distance collapsed
  saturated colours onto grey, because the weighting leans so heavily on the
  green channel.
- **Dithering** makes a photograph read as one and makes flat artwork read as
  noise. Leave it on for photos, off for logos.
- **Vivid** (`-vivid`) weighs hue ahead of lightness, which suits flat art.
- Some colours have no honest neighbour at all. Google's amber yellow sits
  between the palette's lemon and its olive, so every weighting lands on peach
  or olive. **Pinning** (`-map "#F4B400=yellow"`) names the substitution
  instead, and pinned pixels take no dither error so flat areas stay flat.

Dark photographs are worth lifting before they are quantized -- the palette
has no dark greys, only black -- for example with
`ffmpeg -i in.png -vf "eq=brightness=0.18:contrast=1.6:saturation=1.8" out.png`.

## Canvas music

The canvas has its own background track, and `draw` and `record_session` take a
`music` option: `theme-1` (default), `theme-2`, `your-song` — whatever is
currently in the composer — or `off`.

A recording always starts on the tune's first note. Getting there is fiddlier
than it looks: re-picking a track does not rewind it, because the sequencer
keeps running underneath. Switching to silence stops it and switching back
starts the tune again, about 36 frames later, while leaving the SELECT MUSIC
screen takes roughly 32 — so the canvas is up just before the music begins.
The prepared state is then nudged forward to the last silent frame, past the
screen-transition sound effect, and cached.

## What the game allows

The limits are tight and shape everything:

- **Canvas** 248×164, 16 fixed colours, no blending.
- **Songs** up to 96 columns holding at most three notes each.
- **Pitches** are 13 diatonic staff positions, B3 to G5. There are no sharps
  or flats, so imported music gets snapped.

## Command line

`cmd/mcpaint-cli` drives the game directly, without the MCP server.

```sh
# Redraw an image and screenshot the result
go run ./cmd/mcpaint-cli -image picture.png -out out/canvas.png

# Record it being drawn stroke by stroke, from boot, with music
go run ./cmd/mcpaint-cli -image picture.png -strokes -title 3 \
    -video out/picture.mp4 -out out/picture.png -seconds 20

# Flat artwork: no dithering, hue-first matching, brand colours pinned
go run ./cmd/mcpaint-cli -image logo.png -fit contain -dither=false -vivid \
    -map "#4285F4=blue,#F4B400=yellow" -out out/logo.png

# Play a set of drawing operations
go run ./cmd/mcpaint-cli -ops testdata/scene.json -video out/scene.mp4 -full
```

`cmd/mcpaint-song` turns a chord chart into a composition and records it.

`cmd/mcpaint-midi` plays a MIDI file. The staff holds 96 columns, so a longer piece
is split across pages: each is loaded in turn, recorded, and the recordings are
stitched back together with no gap at the seam.

```sh
# See how a piece fits before starting the emulator
go run ./cmd/mcpaint-midi -midi song.mid -dry -auto-key

# Play it, however many staves it takes
go run ./cmd/mcpaint-midi -midi song.mid -auto-key -tempo 24 \
    -instruments "0=star,1=gameboy,2=mario" -out out/song.mp4
```

`-auto-key` is worth reaching for. The staff is strictly diatonic C major, so a
tune in another key has every accidental pulled to a neighbour; shifting the
whole piece can put it in a key the staff actually has. On a Sicilian folk tune
it took the accidentals from 86 down to 19.

### What the staff cannot hold, and what is done about it

Three limits bite, and each has a different answer.

**Range.** Thirteen diatonic positions, B3 to G5, is under two octaves, so a
piece spanning more has to fold. Folding each note on its own lets a part land
wherever the arithmetic reaches, so a rising line jumps down mid-phrase and a
bass ends up above the melody. Each note is instead put in the octave nearest
to where its own part already is, which keeps the shape of the line.

**Three voices per column.** Most of what looks like a shortage is not one:
arrangements double notes at the octave, and once the staff has folded them
they collapse onto the same position. Playing that twice wastes a voice on a
unison. Folding the doublings away recovered nearly everything on the test
piece -- dropped voices went from 50 to 12. What is still too thick keeps the
bass and the melody, which carry the outline, plus a note from the middle where
the chord's character lives; the rest spreads onto the next column, the way a
player would roll a chord by hand. One note in 394 was actually lost.

**No accidentals.** Nothing to be done: the staff has no black keys, so sharps
and flats are pulled to a neighbour. `-auto-key` minimises how often that
happens, but it cannot reach zero for a tune that really does change key.

## Tests

```sh
go test ./...
```

The unit tests in `internal/mp` need neither a ROM nor a core, which is where
most of the logic lives. The end-to-end test in `cmd/mcpaint` drives the
real server over stdio and skips unless `MCPAINT_CORE` and `MCPAINT_ROM`
are set.

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
- Video has to be encoded at the console's own frame rate, 60.0988, not a
  round 30 or 60. Anything else and the picture drifts away from the sound.

Memory addresses come from the labelled RAM map in
[Yoshifanatic1/Mario-Paint-Disassembly](https://github.com/Yoshifanatic1/Mario-Paint-Disassembly)
and were each confirmed against the running game. The palette is decoded from
the game's own `Canvas.bin` rather than sampled from screenshots.
