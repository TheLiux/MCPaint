package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type noteInput struct {
	Column     int    `json:"column" jsonschema:"beat position, 0 to 95"`
	Pitch      string `json:"pitch" jsonschema:"a note name from B3 to G5, or a staff position 1 to 13"`
	Instrument string `json:"instrument" jsonschema:"an instrument name such as mario or yoshi, or a value 0 to 14"`
}

type composeInput struct {
	Notes         []noteInput `json:"notes"`
	Tempo         int         `json:"tempo,omitempty" jsonschema:"1 slowest to 255 fastest, default 18"`
	Loop          bool        `json:"loop,omitempty"`
	TimeSignature string      `json:"timeSignature,omitempty" jsonschema:"3/4 or 4/4, default 4/4"`
}

type composeOutput struct {
	NotesPlaced  int      `json:"notesPlaced"`
	NotesDropped int      `json:"notesDropped"`
	Warnings     []string `json:"warnings,omitempty"`
}

func (s *server) compose(_ context.Context, _ *mcp.CallToolRequest, in composeInput) (*mcp.CallToolResult, composeOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.composer()
	if err != nil {
		return nil, composeOutput{}, err
	}

	song := mp.NewSong()
	if in.Tempo > 0 {
		if in.Tempo > 255 {
			return nil, composeOutput{}, fmt.Errorf("tempo %d is out of range (1..255)", in.Tempo)
		}
		song.Tempo = byte(in.Tempo)
	}
	song.Loop = in.Loop
	switch strings.TrimSpace(in.TimeSignature) {
	case "", "4/4":
		song.TimeSignature = mp.TimeFourFour
	case "3/4":
		song.TimeSignature = mp.TimeThreeFour
	default:
		return nil, composeOutput{}, fmt.Errorf("unknown time signature %q (want 3/4 or 4/4)", in.TimeSignature)
	}

	var out composeOutput
	for i, n := range in.Notes {
		pitch, err := mp.ParsePitch(n.Pitch)
		if err != nil {
			return nil, out, fmt.Errorf("note %d: %w", i, err)
		}
		ins, err := mp.ParseInstrument(n.Instrument)
		if err != nil {
			return nil, out, fmt.Errorf("note %d: %w", i, err)
		}
		if n.Column < 0 || n.Column >= mp.SongColumns {
			return nil, out, fmt.Errorf("note %d: column %d is out of range (0..%d)",
				i, n.Column, mp.SongColumns-1)
		}
		if song.Add(n.Column, pitch, ins) {
			out.NotesPlaced++
		} else {
			out.NotesDropped++
			out.Warnings = append(out.Warnings,
				fmt.Sprintf("column %d already holds three notes; dropped %s on %s",
					n.Column, n.Pitch, n.Instrument))
		}
	}

	sess.SetSong(song)
	sess.RunFrames(10)
	return nil, out, nil
}

type playInput struct {
	Seconds   float64 `json:"seconds,omitempty" jsonschema:"how long to record, default 10"`
	WavPath   string  `json:"wavPath,omitempty" jsonschema:"where to write the audio"`
	VideoPath string  `json:"videoPath,omitempty" jsonschema:"set this to also record the composer screen while it plays"`
}

type playOutput struct {
	WavPath   string  `json:"wavPath"`
	VideoPath string  `json:"videoPath,omitempty"`
	Seconds   float64 `json:"seconds"`
	Silent    bool    `json:"silent" jsonschema:"true when nothing sounded, usually meaning the song is empty"`
}

func (s *server) play(_ context.Context, _ *mcp.CallToolRequest, in playInput) (*mcp.CallToolResult, playOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.composer()
	if err != nil {
		return nil, playOutput{}, err
	}

	secs := in.Seconds
	if secs <= 0 {
		secs = 10
	}
	frames := int(secs * sess.FPS())

	opt := session.PlayOptions{Frames: frames}
	var videoPath string
	if in.VideoPath != "" {
		videoPath, err = s.outPath(in.VideoPath, "song", ".mp4")
		if err != nil {
			return nil, playOutput{}, err
		}
		f := sess.Frame()
		v, err := capture.NewVideo(videoPath, f.Rect.Dx(), f.Rect.Dy(), sess.FPS(), 3)
		if err != nil {
			return nil, playOutput{}, err
		}
		opt.Video = v
		defer v.Close()
	}

	samples, err := sess.Play(opt)
	if err != nil {
		return nil, playOutput{}, err
	}

	wavPath, err := s.outPath(in.WavPath, "song", ".wav")
	if err != nil {
		return nil, playOutput{}, err
	}
	rate := sess.SampleRate()
	if err := capture.WriteWAV(wavPath, samples, rate); err != nil {
		return nil, playOutput{}, err
	}

	var peak int16
	for _, v := range samples {
		if v > peak {
			peak = v
		}
	}
	out := playOutput{
		WavPath:   wavPath,
		VideoPath: videoPath,
		Seconds:   float64(len(samples)/2) / float64(rate),
		Silent:    peak < 64,
	}
	return nil, out, nil
}

type importMIDIInput struct {
	Path            string            `json:"path" jsonschema:"path to a Standard MIDI File"`
	StepsPerQuarter int               `json:"stepsPerQuarter,omitempty" jsonschema:"columns per quarter note, default 2; lower it to fit a longer piece into 96 columns"`
	Transpose       int               `json:"transpose,omitempty" jsonschema:"shift every note by this many semitones before fitting"`
	Tempo           int               `json:"tempo,omitempty" jsonschema:"1 slowest to 255 fastest, default 18"`
	Loop            bool              `json:"loop,omitempty"`
	Instruments     map[string]string `json:"instruments,omitempty" jsonschema:"MIDI channel number to instrument name, e.g. {\"0\": \"mario\"}"`
}

type importMIDIOutput struct {
	*mp.ImportReport
}

func (s *server) importMIDI(_ context.Context, _ *mcp.CallToolRequest, in importMIDIInput) (*mcp.CallToolResult, importMIDIOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.composer()
	if err != nil {
		return nil, importMIDIOutput{}, err
	}

	opt := mp.MIDIOptions{StepsPerQuarter: in.StepsPerQuarter, Transpose: in.Transpose}
	if len(in.Instruments) > 0 {
		opt.Instruments = map[int]byte{}
		for ch, name := range in.Instruments {
			var n int
			if _, err := fmt.Sscanf(ch, "%d", &n); err != nil || n < 0 || n > 15 {
				return nil, importMIDIOutput{}, fmt.Errorf("bad MIDI channel %q (want 0..15)", ch)
			}
			v, err := mp.ParseInstrument(name)
			if err != nil {
				return nil, importMIDIOutput{}, fmt.Errorf("channel %s: %w", ch, err)
			}
			opt.Instruments[n] = v
		}
	}

	song, rep, err := mp.ImportMIDI(in.Path, opt)
	if err != nil {
		return nil, importMIDIOutput{}, err
	}
	if in.Tempo > 0 {
		if in.Tempo > 255 {
			return nil, importMIDIOutput{}, fmt.Errorf("tempo %d is out of range (1..255)", in.Tempo)
		}
		song.Tempo = byte(in.Tempo)
	}
	song.Loop = in.Loop

	sess.SetSong(song)
	sess.RunFrames(10)
	return nil, importMIDIOutput{ImportReport: rep}, nil
}
