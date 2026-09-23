package main

import (
	"context"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ---- shared wire types ----

type pointInput struct {
	X int `json:"x" jsonschema:"x in canvas pixels, 0 at the left edge"`
	Y int `json:"y" jsonschema:"y in canvas pixels, 0 at the top edge"`
}

type opInput struct {
	Kind    string       `json:"kind" jsonschema:"one of pencil, line, rect, ellipse, fill, spray"`
	Points  []pointInput `json:"points" jsonschema:"pencil and spray take a path; line, rect and ellipse take two corners; fill takes one seed point"`
	Color   string       `json:"color" jsonschema:"a palette name such as red, a palette index 0-15, or #RRGGBB matched to the nearest palette entry"`
	Size    int          `json:"size,omitempty" jsonschema:"brush diameter in pixels, default 1"`
	Filled  bool         `json:"filled,omitempty" jsonschema:"fill rect and ellipse rather than outline them"`
	Density int          `json:"density,omitempty" jsonschema:"spray dots per step, default 8"`
}

type imageResult struct {
	Path   string `json:"path" jsonschema:"where the PNG was written"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Note   string `json:"note,omitempty"`
}

// withImage attaches the rendered picture to the result so the caller can
// actually see what it made, not just read a file path.
func withImage(img image.Image, out imageResult) (*mcp.CallToolResult, imageResult, error) {
	data, err := encodePNG(img)
	if err != nil {
		return nil, out, err
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			&mcp.ImageContent{Data: data, MIMEType: "image/png"},
			&mcp.TextContent{Text: fmt.Sprintf("Saved to %s (%dx%d)", out.Path, out.Width, out.Height)},
		},
	}, out, nil
}

// ---- reference ----

type referenceOutput struct {
	CanvasWidth  int      `json:"canvasWidth"`
	CanvasHeight int      `json:"canvasHeight"`
	Colors       []string `json:"colors" jsonschema:"palette names in index order"`
	Instruments  []string `json:"instruments" jsonschema:"instrument names in value order"`
	Pitches      []string `json:"pitches" jsonschema:"note names from the bottom of the staff to the top"`
	SongColumns  int      `json:"songColumns"`
	SongChannels int      `json:"songChannels" jsonschema:"notes that fit in one column"`
	Notes        string   `json:"notes"`
}

func (s *server) reference(context.Context, *mcp.CallToolRequest, struct{}) (*mcp.CallToolResult, referenceOutput, error) {
	pitches := make([]string, 0, mp.MaxPitch)
	for p := byte(mp.MinPitch); p <= mp.MaxPitch; p++ {
		pitches = append(pitches, mp.PitchName(p))
	}
	return nil, referenceOutput{
		CanvasWidth:  mp.VisibleW,
		CanvasHeight: mp.VisibleH,
		Colors:       mp.Colors(),
		Instruments:  mp.Instruments(),
		Pitches:      pitches,
		SongColumns:  mp.SongColumns,
		SongChannels: mp.SongChannels,
		Notes: "Mario Paint's limits are strict: 16 colours with no blending, " +
			"a 248x168 canvas, and songs of at most 96 columns holding three " +
			"notes each. Pitches are diatonic staff positions, so there are no " +
			"sharps or flats.",
	}, nil
}

// ---- drawing ----

type drawImageInput struct {
	ImagePath string `json:"imagePath" jsonschema:"path to a PNG or JPEG to redraw on the canvas"`
	Fit       string `json:"fit,omitempty" jsonschema:"contain (default) fits the whole picture and bands what is left over; cover fills the canvas and crops it; stretch ignores the aspect ratio"`
	Letterbox string `json:"letterbox,omitempty" jsonschema:"palette colour for the bands contain leaves, black by default"`
	Dither    *bool  `json:"dither,omitempty" jsonschema:"Floyd-Steinberg dithering, on by default; turn it off for flat art"`
	OutPath   string `json:"outPath,omitempty" jsonschema:"where to write the resulting PNG"`
}

func (s *server) drawImage(_ context.Context, _ *mcp.CallToolRequest, in drawImageInput) (*mcp.CallToolResult, imageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.canvas()
	if err != nil {
		return nil, imageResult{}, err
	}

	f, err := os.Open(in.ImagePath)
	if err != nil {
		return nil, imageResult{}, err
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return nil, imageResult{}, fmt.Errorf("decoding %s: %w", in.ImagePath, err)
	}

	fit := mp.FitMode(strings.ToLower(in.Fit))
	switch fit {
	case mp.FitContain, mp.FitCover, mp.FitStretch:
	case "":
		fit = mp.FitContain
	default:
		return nil, imageResult{}, fmt.Errorf("unknown fit %q (want contain, cover or stretch)", in.Fit)
	}

	pad := byte(13) // black
	if in.Letterbox != "" {
		v, err := mp.ParseColor(in.Letterbox)
		if err != nil {
			return nil, imageResult{}, fmt.Errorf("letterbox: %w", err)
		}
		pad = v
	}

	c := mp.NewCanvas()
	c.DrawImageWith(src, fit, mp.QuantizeOptions{
		Dither:    in.Dither == nil || *in.Dither,
		Letterbox: &pad,
	})
	sess.SetCanvas(c)
	sess.RunFrames(8)

	path, err := s.outPath(in.OutPath, "canvas", ".png")
	if err != nil {
		return nil, imageResult{}, err
	}
	img := sess.Canvas().ToImage()
	if err := writePNG(path, img); err != nil {
		return nil, imageResult{}, err
	}
	return withImage(img, imageResult{Path: path, Width: mp.VisibleW, Height: mp.VisibleH})
}

type drawInput struct {
	Operations []opInput `json:"operations" jsonschema:"drawing operations, applied in order"`
	Clear      bool      `json:"clear,omitempty" jsonschema:"wipe the canvas before drawing"`
	VideoPath  string    `json:"videoPath,omitempty" jsonschema:"set this to record a timelapse of the drawing appearing, with the canvas music on the soundtrack"`
	Seconds    float64   `json:"seconds,omitempty" jsonschema:"target timelapse length, default 8"`
	Music      string    `json:"music,omitempty" jsonschema:"canvas track for the recording: theme-1 (default), theme-2, your-song or off; a recording always opens on the tune's first note"`
	OutPath    string    `json:"outPath,omitempty" jsonschema:"where to write the resulting PNG"`
	WavPath    string    `json:"wavPath,omitempty" jsonschema:"also write the recorded music on its own"`
}

type drawOutput struct {
	imageResult
	VideoPath string  `json:"videoPath,omitempty"`
	WavPath   string  `json:"wavPath,omitempty"`
	Seconds   float64 `json:"seconds,omitempty"`
}

func (s *server) draw(_ context.Context, _ *mcp.CallToolRequest, in drawInput) (*mcp.CallToolResult, drawOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.setTrack(in.Music); err != nil {
		return nil, drawOutput{}, err
	}

	recording := in.VideoPath != "" || in.WavPath != ""

	// A recording has to start on the downbeat, so the canvas is re-prepared
	// even when the session is already sitting on it.
	var (
		sess *session.Session
		err  error
	)
	if recording {
		sess, err = s.freshCanvas()
	} else {
		sess, err = s.canvas()
	}
	if err != nil {
		return nil, drawOutput{}, err
	}

	ops, err := parseOps(in.Operations)
	if err != nil {
		return nil, drawOutput{}, err
	}
	if in.Clear {
		sess.SetCanvas(mp.NewCanvas())
		sess.RunFrames(4)
	}

	opt := session.DrawOptions{
		TargetSeconds: in.Seconds,
		FPS:           sess.FPS(),
		CaptureAudio:  recording,
	}

	var silentVideo string
	if in.VideoPath != "" {
		f := sess.Frame()
		silentVideo = filepath.Join(os.TempDir(), fmt.Sprintf("mp-draw-%d.mp4", os.Getpid()))
		v, err := capture.NewVideo(silentVideo, f.Rect.Dx(), f.Rect.Dy(), sess.FPS(), 3)
		if err != nil {
			return nil, drawOutput{}, err
		}
		opt.Video = v
		opt.HoldFrames = int(sess.FPS() * 1.5)
	}

	samples, derr := sess.Draw(ops, opt)
	if opt.Video != nil {
		if cerr := opt.Video.Close(); derr == nil {
			derr = cerr
		}
	}
	if derr != nil {
		return nil, drawOutput{}, derr
	}

	out := drawOutput{imageResult: imageResult{
		Path: "", Width: mp.VisibleW, Height: mp.VisibleH,
	}}

	if recording {
		wavPath := in.WavPath
		keepWav := wavPath != ""
		if wavPath == "" {
			wavPath = filepath.Join(os.TempDir(), fmt.Sprintf("mp-draw-%d.wav", os.Getpid()))
		} else if wavPath, err = s.outPath(wavPath, "drawing", ".wav"); err != nil {
			return nil, drawOutput{}, err
		}
		if err := capture.WriteWAV(wavPath, samples, sess.SampleRate()); err != nil {
			return nil, drawOutput{}, err
		}
		if keepWav {
			out.WavPath = wavPath
		} else {
			defer os.Remove(wavPath)
		}

		if in.VideoPath != "" {
			videoPath, err := s.outPath(in.VideoPath, "drawing", ".mp4")
			if err != nil {
				return nil, drawOutput{}, err
			}
			if err := capture.Mux(videoPath, silentVideo, wavPath); err != nil {
				return nil, drawOutput{}, err
			}
			os.Remove(silentVideo)
			out.VideoPath = videoPath
			out.Seconds = capture.Duration(videoPath)
		}
	}

	path, err := s.outPath(in.OutPath, "canvas", ".png")
	if err != nil {
		return nil, drawOutput{}, err
	}
	img := sess.Canvas().ToImage()
	if err := writePNG(path, img); err != nil {
		return nil, drawOutput{}, err
	}
	out.Path = path

	res, _, err := withImage(img, out.imageResult)
	return res, out, err
}

type screenshotInput struct {
	OutPath    string `json:"outPath,omitempty"`
	FullScreen bool   `json:"fullScreen,omitempty" jsonschema:"capture the whole 256x224 screen including the toolbars, instead of just the canvas"`
}

func (s *server) screenshot(_ context.Context, _ *mcp.CallToolRequest, in screenshotInput) (*mcp.CallToolResult, imageResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	sess, err := s.open()
	if err != nil {
		return nil, imageResult{}, err
	}
	if s.current == screenNone {
		if _, err := s.canvas(); err != nil {
			return nil, imageResult{}, err
		}
	}

	var img image.Image
	if in.FullScreen || s.current == screenComposer {
		sess.RunFrames(2)
		img = sess.Frame()
	} else {
		img = sess.Canvas().ToImage()
	}

	path, err := s.outPath(in.OutPath, "screenshot", ".png")
	if err != nil {
		return nil, imageResult{}, err
	}
	if err := writePNG(path, img); err != nil {
		return nil, imageResult{}, err
	}
	b := img.Bounds()
	return withImage(img, imageResult{Path: path, Width: b.Dx(), Height: b.Dy()})
}
