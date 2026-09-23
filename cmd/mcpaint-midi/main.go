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
	"math"
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
		parts  = flag.Bool("auto-parts", false, "work out which channels carry the tune and keep those")
		chans  = flag.String("channels", "", "keep only these MIDI channels, e.g. \"5,1,9\"; empty keeps all")
		drums  = flag.String("drums", "", "instrument for the drum channel; empty drops it, \"auto\" uses the punchiest voice")
		dry    = flag.Bool("dry", false, "report how the piece fits and stop, without starting the emulator")
		auto   = flag.Bool("auto-key", false, "try every transposition and keep the one that needs the fewest accidentals")
	)
	flag.Parse()

	if err := run(*in, *out, *wav, *instr, *steps, *trans, *tempo, *maxPg, *scale, *titleS, *dry, *auto, *drums, *chans, *parts); err != nil {
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

func run(in, out, wav, instr string, steps, trans, tempo, maxPages, scale int, titleSeconds float64, dry, auto bool, drums, chans string, autoParts bool) error {
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

	for _, part := range splitComma(chans) {
		var ch int
		if _, err := fmt.Sscanf(part, "%d", &ch); err != nil || ch < 0 || ch > 15 {
			return fmt.Errorf("bad channel %q (want 0..15)", part)
		}
		opts.Channels = append(opts.Channels, ch)
	}

	switch drums {
	case "":
		// dropped
	case "auto":
		v := mp.PercussionInstrument
		opts.Percussion = &v
	default:
		v, err := mp.ParseInstrument(drums)
		if err != nil {
			return fmt.Errorf("drums: %w", err)
		}
		opts.Percussion = &v
	}

	analysed, err := mp.Parts(in, opts)
	if err != nil {
		return err
	}
	if autoParts && len(opts.Channels) == 0 {
		opts.Channels = mp.SelectParts(analysed, mp.SongChannels, drums != "")
		fmt.Printf("keeping channels %v\n", opts.Channels)
	}

	// The key is chosen after the parts, not before: picking it against
	// channels that are then thrown away optimises for music nobody hears.
	if auto {
		fit, err := mp.BestKey(in, opts)
		if err != nil {
			return err
		}
		fmt.Printf("best key: transpose %+d, melody keeps %.0f%% of its movement (%.0f%% flattened, %d accidentals)\n",
			fit.Transpose, fit.Preserved, fit.Flattened, fit.Accidental)
		opts.Transpose = fit.Transpose
	}
	pages, rep, err := mp.ImportMIDIPages(in, opts, maxPages)
	if err != nil {
		return err
	}

	fmt.Printf("%s: %d notes read, %d placed across %d staves\n",
		filepath.Base(in), rep.NotesRead, rep.NotesPlaced, rep.Pages)
	fmt.Printf("  transposed %d, snapped %d, doublings folded %d, spread %d\n",
		rep.Transposed, rep.Snapped, rep.Doubled, rep.Spread)
	fmt.Printf("  dropped %d voices and %d past the end\n",
		rep.DroppedVoices, rep.DroppedLength)
	for _, w := range rep.Warnings {
		fmt.Println("  " + w)
	}

	if chans, err := mp.Channels(in, opts); err == nil {
		voices := map[int]string{}
		for _, c := range chans {
			voices[c.Channel] = c.Instrument
		}
		kept := map[int]bool{}
		for _, ch := range opts.Channels {
			kept[ch] = true
		}
		fmt.Println("  ch  notes  range     line   pace   part          voice")
		for _, p := range analysed {
			mark := " "
			if len(opts.Channels) == 0 || kept[p.Channel] {
				mark = "*"
			}
			fmt.Printf("  %s%2d %6d  %3d..%-3d  %5.2f  %5.1f   %-12s  %s\n",
				mark, p.Channel, p.Notes, p.Low, p.High,
				p.Polyphony, p.Density, p.Role, voices[p.Channel])
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

	// Every page shares one tempo, so one measurement of where the staff starts
	// and how long a column lasts cuts them all to their exact length.
	var lead, period float64
	var all []int16
	for i, song := range pages {
		song.Tempo = byte(tempo)
		s.Click(session.ComposerClearX, session.ComposerClearY)
		s.RunFrames(30)
		s.SetSong(song)
		s.RunFrames(10)

		if i == 0 {
			if lead, period, err = s.ColumnTiming(); err != nil {
				return err
			}
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
		// Frames are rounded against the running total, not per page, so the
		// fractions do not add up to drift over a long piece.
		frames := 0
		if i < len(pages)-1 {
			frames = int(math.Round(float64((i+1)*mp.SongColumns)*period)) -
				int(math.Round(float64(i*mp.SongColumns)*period))
		}
		samples, err := s.PlayWindow(int(math.Round(lead)), frames, v)
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
