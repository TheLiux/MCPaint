// Command mcpaint-midi plays a MIDI file through Mario Paint's composer.
//
// The staff holds 96 columns, so anything longer is split across several
// pages: each is loaded in turn, recorded, and the recordings are stitched
// back into one piece.
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

func main() {
	var (
		in     = flag.String("midi", "", "Standard MIDI File to play")
		out    = flag.String("out", "out/midi.mp4", "video destination")
		wav    = flag.String("wav", "out/midi.wav", "audio destination")
		steps  = flag.Int("steps", 2, "columns per quarter note")
		trans  = flag.Int("transpose", 0, "semitones to shift before fitting")
		tempo  = flag.Int("tempo", 24, "1 slowest .. 255 fastest")
		maxPg  = flag.Int("pages", 0, "cap the number of staves; 0 for as many as it takes")
		instr  = flag.String("instruments", "", `per channel, e.g. "0=mario,1=gameboy,2=star"`)
		scale  = flag.Int("scale", 3, "video upscale factor")
		titleS = flag.Float64("title", 0, "seconds of title screen before the music")
		dry    = flag.Bool("dry", false, "report how the piece fits and stop, without starting the emulator")
		auto   = flag.Bool("auto-key", false, "try every transposition and keep the one that needs the fewest accidentals")
	)
	flag.Parse()

	if err := run(*in, *out, *wav, *instr, *steps, *trans, *tempo, *maxPg, *scale, *titleS, *dry, *auto); err != nil {
		log.Fatal(err)
	}
}

func parseInstruments(spec string) (map[int]byte, error) {
	if spec == "" {
		return nil, nil
	}
	out := map[int]byte{}
	for _, part := range splitComma(spec) {
		var ch int
		var name string
		if _, err := fmt.Sscanf(part, "%d=%s", &ch, &name); err != nil {
			return nil, fmt.Errorf("bad instrument mapping %q (want channel=name)", part)
		}
		v, err := mp.ParseInstrument(name)
		if err != nil {
			return nil, err
		}
		out[ch] = v
	}
	return out, nil
}

func splitComma(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == ',' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

func run(in, out, wav, instr string, steps, trans, tempo, maxPages, scale int, titleSeconds float64, dry, auto bool) error {
	if in == "" {
		return fmt.Errorf("pass -midi")
	}
	if tempo < 1 || tempo > 255 {
		return fmt.Errorf("tempo %d is out of range (1..255)", tempo)
	}

	instruments, err := parseInstruments(instr)
	if err != nil {
		return err
	}

	opts := mp.MIDIOptions{
		StepsPerQuarter: steps,
		Transpose:       trans,
		Instruments:     instruments,
	}

	if auto {
		best, snapped, err := bestKey(in, opts, maxPages)
		if err != nil {
			return err
		}
		fmt.Printf("best key: transpose %+d, %d accidentals to snap\n", best, snapped)
		opts.Transpose = best
	}

	pages, rep, err := mp.ImportMIDIPages(in, opts, maxPages)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %d notes read, %d placed across %d staves\n",
		filepath.Base(in), rep.NotesRead, rep.NotesPlaced, rep.Pages)
	fmt.Printf("  transposed %d, snapped %d, dropped %d voices and %d past the end\n",
		rep.Transposed, rep.Snapped, rep.DroppedVoices, rep.DroppedLength)
	for _, w := range rep.Warnings {
		fmt.Println("  " + w)
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

	tmp, err := os.MkdirTemp("", "mcpaint-midi-")
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
	for i, song := range pages {
		song.Tempo = byte(tempo)
		s.Click(session.ComposerClearX, session.ComposerClearY)
		s.RunFrames(30)
		s.SetSong(song)
		s.RunFrames(10)

		if i == 0 {
			f, err := os.Create(filepath.Join(filepath.Dir(out), "staff.png"))
			if err == nil {
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
			fmt.Printf("  page %d is silent, skipping\n", i+1)
			os.Remove(video)
			continue
		}

		audio := filepath.Join(tmp, fmt.Sprintf("page%02d.wav", i))
		if err := capture.WriteWAV(audio, samples, s.SampleRate()); err != nil {
			return err
		}
		all = append(all, samples...)
		parts = append(parts, capture.Part{Video: video, Audio: audio})
		fmt.Printf("  page %d: %d notes, %.1fs\n",
			i+1, len(song.Notes), float64(len(samples)/2)/float64(s.SampleRate()))
	}

	if err := capture.WriteWAV(wav, all, s.SampleRate()); err != nil {
		return err
	}

	// Pages are cut hard against each other so the music runs on; only the
	// very start and end of the piece fade.
	if err := capture.JoinWith(out, parts, capture.JoinOptions{
		FadeSeconds: 0.5, OpenCold: true, EndCold: true, SeamFades: false,
	}); err != nil {
		return err
	}

	fmt.Printf("wrote %s (%.1fs) and %s\n", out, capture.Duration(out), wav)
	return nil
}

// bestKey finds the transposition that leaves the fewest notes off the staff.
//
// The staff is strictly diatonic C major, so a tune in another key has every
// accidental pulled to a neighbour. Shifting the whole piece can put it in a
// key the staff actually has, which is far kinder than snapping note by note.
func bestKey(path string, opts mp.MIDIOptions, maxPages int) (int, int, error) {
	bestShift, bestSnapped := 0, -1
	for shift := -11; shift <= 11; shift++ {
		try := opts
		try.Transpose = shift
		_, rep, err := mp.ImportMIDIPages(path, try, maxPages)
		if err != nil {
			return 0, 0, err
		}
		// Prefer fewer accidentals, then the smallest shift.
		if bestSnapped < 0 || rep.Snapped < bestSnapped ||
			(rep.Snapped == bestSnapped && abs(shift) < abs(bestShift)) {
			bestShift, bestSnapped = shift, rep.Snapped
		}
	}
	return bestShift, bestSnapped, nil
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
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
