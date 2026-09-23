// Command mcpaint-song builds a chord chart into a Mario Paint composition and
// records it.
//
// The staff holds 96 columns and one tempo, so the chart is laid out across
// pages that break wherever the tempo changes. Each page is loaded, recorded,
// and the recordings are stitched back into one piece.
package main

import (
	"flag"
	"fmt"
	"image/png"
	"log"
	"os"
	"path/filepath"

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

// chord is one bar, already reduced to notes the staff can hold. When the
// written chord had an altered note the staff has no key for, as changes.
type chord struct {
	label string
	as    string
	tones []string // root first
}

func c(label string, tones ...string) chord { return chord{label: label, tones: tones} }

func sub(label, as string, tones ...string) chord {
	return chord{label: label, as: as, tones: tones}
}

// section is a run of bars sharing a tempo. Pages break between sections, so
// each can move at its own pace.
type section struct {
	name   string
	tempo  byte
	chords []chord
}

// Chords used by the chart, reduced where the staff cannot follow.
var (
	cMaj  = c("DO", "C", "E", "G")
	eMin  = c("MIm", "E", "G", "B")
	aMin7 = c("LAm7", "A", "C", "E", "G")
	g6    = c("SOL6", "G", "B", "D", "E")
	dMin  = c("REm", "D", "F", "A")
	g     = c("SOL", "G", "B", "D")
	f     = c("FA", "F", "A", "C")
	g7    = c("SOL7", "G", "B", "D", "F")
	g11   = c("SOL11", "G", "C", "D", "F")
	cAdd9 = c("DOadd9", "C", "D", "E", "G")
	cOnE  = c("DO/MI", "E", "C", "G")

	// The altered chords: the staff has no sharps or flats, so each loses its
	// altered note and keeps the rest.
	dMinMaj7 = sub("REm7+", "REm7", "D", "F", "A", "C")
	gAug     = sub("SOL5+", "SOL", "G", "B", "D")
	a7       = sub("LA7", "LAm7", "A", "C", "E", "G")
	c7       = sub("DO7", "DO", "C", "E", "G")
	fMin     = sub("FAm", "FA", "F", "A", "C")
)

func verse() []chord {
	return []chord{
		cMaj, eMin, aMin7, dMin,
		dMinMaj7, g, gAug, cMaj,
		eMin, aMin7, a7, dMin,
		dMinMaj7, g, gAug, cMaj,
	}
}

func chorus() []chord {
	return []chord{
		f, cMaj, a7, dMin,
		g7, cMaj, dMin, cOnE, c7,
		f, cMaj, a7, dMin,
		fMin, g, g11,
	}
}

// chart lays the piece out. The tempos give the verses a walking pace, lift
// the chorus, and let the ending settle.
var chart = []section{
	{"intro", 16, []chord{cMaj, eMin, aMin7, g6}},
	{"verse 1", 22, verse()},
	{"chorus 1", 30, chorus()},
	{"verse 2", 22, verse()},
	{"chorus 2", 30, chorus()},
	{"outro", 14, []chord{cMaj, eMin, aMin7, dMin, dMinMaj7, g, cAdd9}},
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

const barColumns = 4

type page struct {
	song    *mp.Song
	section string
	bars    int
}

// build lays the chart out across pages, breaking wherever the tempo changes
// because a staff holds only one.
func build(bass, harm, mel byte) ([]page, []string) {
	var (
		pages   []page
		changed []string
		melody  = byte(9) // start the melody around C5
	)

	for _, sec := range chart {
		song := mp.NewSong()
		song.Tempo = sec.tempo
		col, bars := 0, 0

		for _, ch := range sec.chords {
			if col+barColumns > mp.SongColumns {
				pages = append(pages, page{song, sec.name, bars})
				song = mp.NewSong()
				song.Tempo = sec.tempo
				col, bars = 0, 0
			}
			if ch.as != "" {
				changed = append(changed, fmt.Sprintf("%s played as %s", ch.label, ch.as))
			}

			root, third, fifth := ch.tones[0], ch.tones[1], ch.tones[2]
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
			m1 := nearest(third, melody)
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
			melody = m2

			col += barColumns
			bars++
		}
		if bars > 0 {
			pages = append(pages, page{song, sec.name, bars})
		}
	}
	return pages, changed
}

func main() {
	var (
		out     = flag.String("out", "out/song.mp4", "video destination")
		wav     = flag.String("wav", "out/song.wav", "audio destination")
		still   = flag.String("still", "out/song.png", "staff screenshot")
		bassIns = flag.String("bass", "gameboy", "bass instrument")
		harmIns = flag.String("harmony", "mario", "harmony instrument")
		melIns  = flag.String("melody", "star", "melody instrument")
		scale   = flag.Int("scale", 3, "video upscale factor")
		titleS  = flag.Float64("title", 0, "seconds of title screen before the music")
		dry     = flag.Bool("dry", false, "report the layout and stop, without starting the emulator")
	)
	flag.Parse()

	if err := run(*out, *wav, *still, *bassIns, *harmIns, *melIns, *scale, *titleS, *dry); err != nil {
		log.Fatal(err)
	}
}

func run(out, wav, still, bassIns, harmIns, melIns string, scale int, titleSeconds float64, dry bool) error {
	bass, err := mp.ParseInstrument(bassIns)
	if err != nil {
		return err
	}
	harm, err := mp.ParseInstrument(harmIns)
	if err != nil {
		return err
	}
	mel, err := mp.ParseInstrument(melIns)
	if err != nil {
		return err
	}

	pages, changed := build(bass, harm, mel)

	notes, bars := 0, 0
	for _, p := range pages {
		notes += len(p.song.Notes)
		bars += p.bars
	}
	fmt.Printf("%d bars, %d notes, %d pages\n", bars, notes, len(pages))
	for _, p := range pages {
		fmt.Printf("  %-9s %2d bars at tempo %d\n", p.section, p.bars, p.song.Tempo)
	}
	if len(changed) > 0 {
		seen := map[string]bool{}
		fmt.Println("chords the staff could not hold:")
		for _, c := range changed {
			if !seen[c] {
				seen[c] = true
				fmt.Println("  " + c)
			}
		}
	}
	if dry {
		return nil
	}

	s, err := session.Open(session.Config{
		CorePath: os.Getenv("MCPAINT_CORE"),
		ROMPath:  os.Getenv("MCPAINT_ROM"),
	})
	if err != nil {
		return err
	}
	defer s.Close()

	tmp, err := os.MkdirTemp("", "mcpaint-song-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	os.MkdirAll(filepath.Dir(out), 0o755)

	fps := s.FPS()
	var parts []capture.Part

	if titleSeconds > 0 {
		part, err := recordTitle(s, tmp, fps, scale, titleSeconds)
		if err != nil {
			return err
		}
		parts = append(parts, part)
	}

	if err := s.OpenComposer(); err != nil {
		return err
	}

	var all []int16
	for i, p := range pages {
		s.Click(session.ComposerClearX, session.ComposerClearY)
		s.RunFrames(30)
		s.SetSong(p.song)
		s.RunFrames(10)

		if i == 0 && still != "" {
			if f, err := os.Create(still); err == nil {
				png.Encode(f, s.Frame())
				f.Close()
			}
		}

		video := filepath.Join(tmp, fmt.Sprintf("page%02d.mp4", i))
		fr := s.Frame()
		v, err := capture.NewVideo(video, fr.Rect.Dx(), fr.Rect.Dy(), fps, scale)
		if err != nil {
			return err
		}
		samples, err := s.PlayMeasured(session.PlayOptions{Video: v})
		if cerr := v.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return err
		}
		if len(samples) == 0 {
			os.Remove(video)
			continue
		}

		audio := filepath.Join(tmp, fmt.Sprintf("page%02d.wav", i))
		if err := capture.WriteWAV(audio, samples, s.SampleRate()); err != nil {
			return err
		}
		all = append(all, samples...)
		parts = append(parts, capture.Part{Video: video, Audio: audio})
		fmt.Printf("  %-9s %.1fs\n", p.section, float64(len(samples)/2)/float64(s.SampleRate()))
	}

	if err := capture.WriteWAV(wav, all, s.SampleRate()); err != nil {
		return err
	}

	// Pages are cut hard against each other so the music runs on; only the
	// very start and end of the piece fade.
	if err := capture.JoinWith(out, parts, capture.JoinOptions{
		FadeSeconds: 0.5, OpenCold: true, EndCold: true,
	}); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%.1fs) and %s\n", out, capture.Duration(out), wav)
	return nil
}

func recordTitle(s *session.Session, tmp string, fps float64, scale int, seconds float64) (capture.Part, error) {
	if err := s.PrepareTitle(); err != nil {
		return capture.Part{}, err
	}
	video := filepath.Join(tmp, "title.mp4")
	audio := filepath.Join(tmp, "title.wav")

	fr := s.Frame()
	v, err := capture.NewVideo(video, fr.Rect.Dx(), fr.Rect.Dy(), fps, scale)
	if err != nil {
		return capture.Part{}, err
	}
	s.Core().StartAudioCapture()
	for i := 0; i < int(seconds*fps); i++ {
		s.Run()
		if err := v.Write(s.Frame()); err != nil {
			return capture.Part{}, err
		}
	}
	samples := s.Core().StopAudioCapture()
	if err := v.Close(); err != nil {
		return capture.Part{}, err
	}
	if err := capture.WriteWAV(audio, samples, s.SampleRate()); err != nil {
		return capture.Part{}, err
	}
	return capture.Part{Video: video, Audio: audio}, nil
}
