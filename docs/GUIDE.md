# Guide

A walk through the practical side: getting set up, turning a picture into a
drawing that holds up, and turning a MIDI file into something that still sounds
like the song. The [README](../README.md) explains how things work underneath;
this is about what to do.

## Setup

You need your own Mario Paint ROM, a libretro SNES core (snes9x works) and
`ffmpeg` on the PATH.

```sh
export MCPAINT_CORE="$HOME/Library/Application Support/RetroArch/cores/snes9x_libretro.dylib"
export MCPAINT_ROM="/path/to/Mario Paint (JU).smc"

go build -o build/ ./cmd/...
```

That leaves four programs in `build/`:

| Program | Takes | Gives |
|---|---|---|
| `mcpaint-cli` | a picture, or a list of drawing operations | a canvas PNG, optionally a timelapse MP4 |
| `mcpaint-midi` | a MIDI file | the composer playing it, as MP4 and WAV |
| `mcpaint-song` | the chord chart built into it | the same, laid out by section |
| `mcpaint` | nothing: it is the MCP server | the tools, for an AI client to call |

Build once and call the binaries. `go run` recompiles on every call, which is
fine while working on the code and slow otherwise.

## Pictures

The canvas is 248×164 with sixteen fixed colours. Everything below comes down
to those two numbers.

### Framing

The canvas is wider than most photos: about 3:2, while a phone portrait is 9:16
and a profile picture is square. `-fit` decides what gives:

- `cover` fills the canvas and crops the overflow, **centred**. On a portrait
  that usually takes the top of the head.
- `contain` keeps the whole picture and leaves bands at the sides.
- `stretch` distorts it; rarely what you want.

For anything with a subject, crop it yourself first, to 248:164, around what
matters. `ffmpeg` does it in one line; `crop=w:h:x:y` takes the top-left corner:

```sh
# a 1730-pixel-wide photo: 1730×1144 is 248:164, starting 170 px down
ffmpeg -i photo.jpg -vf "crop=1730:1144:0:170" framed.png
```

When you use `contain`, pad with the picture's own background instead of
leaving bands. Sample a corner, then pad to the canvas ratio:

```sh
ffmpeg -i sprite.png -vf "crop=1:1:5:5,format=rgb24" -f rawvideo - | xxd -p   # e.g. 7d0000
ffmpeg -i sprite.png -vf "pad=968:640:164:0:color=0x7d0000" padded.png         # 640 tall → 968 wide
```

### Matching the colours

| Kind of picture | Flags | Why |
|---|---|---|
| Photograph | defaults (dithering on) | Dithering is what makes sixteen colours read as a photo. |
| Anime, illustration, logo | `-vivid` | Hue first: a red stays red rather than becoming a brown of the right lightness. |
| Pixel art, flat icons | `-dither=false -vivid` | Every block stays one colour; dithering would turn flat areas into noise. |
| A specific colour must land somewhere | `-map "#4285F4=blue"` | Pins it; pinned pixels take no dither error. |

The palette has gaps you cannot flag your way out of. There is **no purple**
(purple hair comes out magenta with blue dotted in), **no dark red** (it goes
to red or orange), and **no dark greys**, only black. A dark photo is worth
lifting before it goes in:

```sh
ffmpeg -i dark.jpg -vf "eq=brightness=0.18:contrast=1.6:saturation=1.8" lifted.png
```

### Recording it being drawn

```sh
build/mcpaint-cli -image framed.png -fit cover \
    -strokes -title 3 -seconds 20 -music theme-1 \
    -video out/picture.mp4 -out out/picture.png
```

- `-strokes` replays the picture stroke by stroke with the cursor moving,
  instead of pasting it in one go.
- `-seconds` is the length of the drawing, whatever the stroke count. A photo
  takes around 30,000 strokes, pixel art a few hundred.
- `-title` adds that many seconds of the machine booting to the title screen.
- `-music` is the canvas track: `theme-1`, `theme-2`, `your-song` (whatever is
  in the composer) or `off`.

### Drawing by hand

`-ops` takes a JSON list of operations drawn with the game's own tools;
[testdata/scene.json](../testdata/scene.json) is a whole scene. Each operation
has a `kind`, its `points`, a palette `color` (0–15) and, depending on the kind,
a `size`, `filled` or `density`:

| Kind | Points |
|---|---|
| `pencil` | a freehand line through all of them |
| `line` | from the first to the second |
| `rect`, `ellipse` | the two corners of the bounds; `"filled": true` to fill |
| `fill` | a flood fill from the first |
| `spray` | the spray can along all of them |

## Music

The composer is much narrower than a MIDI file: 96 columns per staff, three
notes per column, thirteen staff positions from B3 to G5, and no sharps or
flats. `mcpaint-midi` handles the rest: it splits the piece across as many staves as
it needs and fits the notes in. It is still worth knowing what it had to do.

### Always look first

```sh
build/mcpaint-midi -midi song.mid -auto-key -dry
```

`-dry` reports how the piece fits without starting the emulator. Read the
report before recording:

| Line | Healthy | If it is high |
|---|---|---|
| `transposed` | a handful | Whole lines are being folded by octaves. Try another key (below). |
| `snapped` | near zero | Sharps and flats pulled to a neighbour. Try another key. |
| `doublings folded` | anything | Harmless: octave doublings merged onto one note. |
| `spread` / `dropped` | near zero | Chords too thick for three voices. Keep fewer channels. |

The channel table under it shows what each channel was taken for (melody, bass,
inner line, chords, percussion) and which instrument it got.

### Key

The staff is C major and nothing else. `-auto-key` tries every shift and keeps
the one that best preserves the melody's rises and falls. It is a good start,
not the last word: compare its choice with the same key an octave either side,
`-transpose N`, `N-12` and `N+12`, and keep whichever has the fewest
`transposed` notes. A tune whose range sits just above the staff gets folded
note by note in one octave and fits almost untouched in the next.

### Resolution

`-steps` is columns per quarter note. `2` resolves eighth notes; if the tune
has sixteenths, dotted rhythms or fast runs, use `4`, or they get merged. More
steps means more staves: a four-minute song at `4` fills a dozen.

### Tempo

`-tempo` runs from 1 to 255 and is the game's own unit, not beats per minute.
A column lasts about 4.3 / tempo seconds, which works out to

```
tempo ≈ BPM × steps ÷ 14
```

| BPM | `-steps 2` | `-steps 4` |
|---|---|---|
| 90 | 13 | 26 |
| 120 | 17 | 34 |
| 140 | 20 | 40 |

A MIDI file without a tempo event is assumed to be 120.

### Dense arrangements

A piano arrangement or a full band does not fit in three voices. The fitter
keeps bass and melody first and spreads what is left, but on a busy piece you
get a better result by choosing yourself: `-channels "0,1"` keeps just those
MIDI channels. The dry-run table tells you which is which.

If the melody and its accompaniment share one channel, as both hands of a piano
often do, no channel choice separates them; reduce the file to its top line and
bass in a MIDI editor first.

### Instruments

`-instruments "0=star,1=mario"` voices channels by name. Left alone, channels
are voiced by register, darkest instrument on the lowest part. The fifteen are
`mario`, `mushroom`, `yoshi`, `star`, `flower`, `gameboy`, `cat`, `dog`,
`pig`, `swan`, `face`, `plane`, `boat`, `car` and `heart`. `star` carries a
melody well, `mario` makes a soft bass, and `gameboy` is a square wave that
gets tiring over a whole song.

Drums on General MIDI channel 10 are dropped unless you ask for them:
`-drums auto` lays them on three positions for low, middle and high.

### Recording

```sh
build/mcpaint-midi -midi song.mid -transpose -8 -steps 4 -tempo 34 \
    -instruments "0=star,1=mario" -title 3 \
    -out out/song.mp4 -wav out/song.wav
```

Each staff is loaded, played and recorded in turn, then joined. Every page is
cut to its exact length, measured from the game at the chosen tempo, so rests
that fall across a page break are kept and the beat does not jump at the seams.

## From an AI client

Register the server and ask for things in plain language instead:

```sh
claude mcp add mcpaint \
    -e MCPAINT_CORE="…" -e MCPAINT_ROM="…" \
    -- /path/to/build/mcpaint
```

The tools are listed in the [README](../README.md#tools). `reference` returns
the palette, instruments and limits, which is the first thing a model should
read.

## Troubleshooting

| Symptom | Cause |
|---|---|
| The top of a head is missing | `-fit cover` crops centred. Crop to 248:164 yourself first. |
| White bands beside the picture | `-fit contain` on a picture of another ratio. Pad it with its background. |
| The melody lurches up and down | Folded by octaves. Look at `transposed` in `-dry` and try a key 12 away. |
| Fast passages sound smeared | `-steps 2` on sixteenth notes. Use `-steps 4`. |
| The song is far too fast or slow | Tempo is not BPM; use the formula above. |
| The video drifts from the sound | It was re-encoded at 30 or 60 fps. Keep the console's own 60.0988. |
