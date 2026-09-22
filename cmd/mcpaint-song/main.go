// Command mcpaint-song builds a chord chart into a Mario Paint composition and records it.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"

	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
)

// The staff is thirteen diatonic positions, B3 at the bottom to G5 at the top,
// so every pitch is a white key and every chord has to be one too.
var pitchOf = map[string][]byte{
	"B": {1, 8},
	"C": {2, 9},
	"D": {3, 10},
	"E": {4, 11},
	"F": {5, 12},
	"G": {6, 13},
	"A": {7},
}

// chord is one bar of the chart, already reduced to notes the staff can hold.
type chord struct {
	label string   // as written on the chart
	as    string   // what it became, when it had to change
	tones []string // root first
}

func c(label string, tones ...string) chord { return chord{label: label, tones: tones} }

func sub(label, as string, tones ...string) chord {
	return chord{label: label, as: as, tones: tones}
}

// The chart: intro, two verses, the chorus line and the closing bars.
var chart = []chord{
	// INTRO
	c("DO", "C", "E", "G"),
	c("MIm", "E", "G", "B"),
	c("LAm7", "A", "C", "E", "G"),
	c("SOL6", "G", "B", "D", "E"),

	// Che bedda Catania, Catania di notti
	c("DO", "C", "E", "G"),
	c("MIm", "E", "G", "B"),
	c("LAm7", "A", "C", "E", "G"),
	c("REm", "D", "F", "A"),
	sub("REm7+", "REm7", "D", "F", "A", "C"),
	c("SOL", "G", "B", "D"),
	sub("SOL5+", "SOL", "G", "B", "D"),
	c("DO", "C", "E", "G"),

	// Poi l'alba d'argento s'ammisca cco mari
	c("MIm", "E", "G", "B"),
	c("LAm7", "A", "C", "E", "G"),
	sub("LA7", "LAm7", "A", "C", "E", "G"),
	c("REm", "D", "F", "A"),
	sub("REm7+", "REm7", "D", "F", "A", "C"),
	c("SOL", "G", "B", "D"),
	sub("SOL5+", "SOL", "G", "B", "D"),
	c("DO", "C", "E", "G"),

	// Musica, c'é musica, Catania abballa
	c("FA", "F", "A", "C"),
	c("DO", "C", "E", "G"),
	c("SOL7", "G", "B", "D", "F"),
	c("DO", "C", "E", "G"),
}

// nearest picks the octave of a note closest to where the melody already is,
// which keeps the line stepwise instead of leaping about.
func nearest(note string, to byte) byte {
	opts := pitchOf[note]
	best, bestD := opts[0], 99
	for _, p := range opts {
		d := int(p) - int(to)
		if d < 0 {
			d = -d
		}
		if d < bestD {
			best, bestD = p, d
		}
	}
	return best
}

func low(note string) byte  { return pitchOf[note][0] }
func high(note string) byte { return pitchOf[note][len(pitchOf[note])-1] }

func main() {
	var (
		out     = flag.String("out", "out/catania.mp4", "video destination")
		wav     = flag.String("wav", "out/catania.wav", "audio destination")
		still   = flag.String("still", "out/catania.png", "staff screenshot")
		tempo   = flag.Int("tempo", 20, "1 slowest .. 255 fastest")
		secs    = flag.Float64("seconds", 30, "how long to record")
		bassIns = flag.String("bass", "gameboy", "bass instrument")
		harmIns = flag.String("harmony", "mario", "harmony instrument")
		melIns  = flag.String("melody", "star", "melody instrument")
	)
	flag.Parse()

	bass, err := mp.ParseInstrument(*bassIns)
	if err != nil {
		log.Fatal(err)
	}
	harm, err := mp.ParseInstrument(*harmIns)
	if err != nil {
		log.Fatal(err)
	}
	mel, err := mp.ParseInstrument(*melIns)
	if err != nil {
		log.Fatal(err)
	}

	song := mp.NewSong()
	song.Tempo = byte(*tempo)

	melodyAt := byte(9) // start the melody around C5
	col := 0
	var changed []string

	for _, ch := range chart {
		if col+4 > mp.SongColumns {
			break
		}
		if ch.as != "" {
			changed = append(changed, fmt.Sprintf("%s played as %s", ch.label, ch.as))
		}

		root, third := ch.tones[0], ch.tones[1]
		fifth := ch.tones[2]
		colour := ch.tones[len(ch.tones)-1]

		// Bass on the strong beats.
		song.Notes = append(song.Notes,
			mp.Note{Column: col, Channel: 0, Pitch: low(root), Instrument: bass},
			mp.Note{Column: col + 2, Channel: 0, Pitch: low(fifth), Instrument: bass},
		)

		// Harmony fills the off-beats.
		song.Notes = append(song.Notes,
			mp.Note{Column: col + 1, Channel: 1, Pitch: nearest(third, 6), Instrument: harm},
			mp.Note{Column: col + 3, Channel: 1, Pitch: nearest(colour, 6), Instrument: harm},
		)

		// Melody moves to the nearest chord tone, then to its neighbour.
		m1 := nearest(third, melodyAt)
		if m1 < 8 {
			m1 = high(third)
		}
		m2 := nearest(colour, m1)
		if m2 < 8 {
			m2 = high(colour)
		}
		song.Notes = append(song.Notes,
			mp.Note{Column: col, Channel: 2, Pitch: m1, Instrument: mel},
			mp.Note{Column: col + 2, Channel: 2, Pitch: m2, Instrument: mel},
		)
		melodyAt = m2
		col += 4
	}

	fmt.Printf("%d bars, %d notes, %d columns used\n", len(chart), len(song.Notes), col)
	if len(changed) > 0 {
		fmt.Println("chords the staff could not hold:")
		for _, c := range changed {
			fmt.Println("  " + c)
		}
	}

	s, err := session.Open(session.Config{
		CorePath: os.Getenv("MCPAINT_CORE"),
		ROMPath:  os.Getenv("MCPAINT_ROM"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if err := s.OpenComposer(); err != nil {
		log.Fatal(err)
	}
	os.MkdirAll("out", 0o755)

	s.Click(session.ComposerClearX, session.ComposerClearY)
	s.RunFrames(40)
	s.SetSong(song)
	s.RunFrames(20)

	fh, _ := os.Create(*still)
	png.Encode(fh, s.Frame())
	fh.Close()

	v, err := capture.NewVideo(*out+".tmp.mp4", 256, 224, s.FPS(), 3)
	if err != nil {
		log.Fatal(err)
	}
	samples, err := s.Play(session.PlayOptions{Frames: int(*secs * s.FPS()), Video: v})
	if err != nil {
		log.Fatal(err)
	}
	if err := v.Close(); err != nil {
		log.Fatal(err)
	}
	if err := capture.WriteWAV(*wav, samples, s.SampleRate()); err != nil {
		log.Fatal(err)
	}
	if err := capture.JoinWith(*out, []capture.Part{{Video: *out + ".tmp.mp4", Audio: *wav}},
		capture.JoinOptions{FadeSeconds: 0.6, OpenCold: true, EndCold: true}); err != nil {
		log.Fatal(err)
	}
	os.Remove(*out + ".tmp.mp4")

	fmt.Printf("wrote %s (%.1fs), %s and %s\n",
		*out, capture.Duration(*out), *wav, *still)
}
