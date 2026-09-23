// Command mcpaint-cli drives Mario Paint directly, without going through the MCP
// server. It draws a picture or a list of operations onto the canvas and
// captures a screenshot, a recording, or both.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
)

type config struct {
	core, rom            string
	image, opsFile       string
	out, video, wav      string
	fit, rules, music    string
	dither, vivid, full  bool
	strokes              bool
	titleSeconds, target float64
	fps                  float64
	scale                int
}

func main() {
	var c config
	flag.StringVar(&c.core, "core", os.Getenv("MCPAINT_CORE"), "libretro core")
	flag.StringVar(&c.rom, "rom", os.Getenv("MCPAINT_ROM"), "Mario Paint ROM")
	flag.StringVar(&c.image, "image", "", "image to redraw on the canvas")
	flag.StringVar(&c.opsFile, "ops", "", "JSON file of drawing operations")
	flag.StringVar(&c.out, "out", "out/canvas.png", "screenshot destination")
	flag.StringVar(&c.video, "video", "", "record to this MP4")
	flag.StringVar(&c.wav, "wav", "", "also write the recorded music on its own")
	flag.StringVar(&c.fit, "fit", "contain", "contain | cover | stretch")
	flag.StringVar(&c.rules, "map", "", `pin source colours, e.g. "#4285F4=blue,#F4B400=yellow"`)
	flag.StringVar(&c.music, "music", "theme-1", "canvas track: theme-1, theme-2, your-song or off")
	flag.BoolVar(&c.dither, "dither", true, "Floyd-Steinberg dithering")
	flag.BoolVar(&c.vivid, "vivid", false, "match hue ahead of lightness; suits flat artwork")
	flag.BoolVar(&c.full, "full", false, "capture the whole screen, not just the canvas")
	flag.BoolVar(&c.strokes, "strokes", false, "replay an imported image as strokes instead of pasting it")
	flag.Float64Var(&c.titleSeconds, "title", 0, "seconds of title screen to record before the drawing")
	flag.Float64Var(&c.target, "seconds", 8, "target length of the drawing")
	flag.Float64Var(&c.fps, "fps", 0, "video frame rate; 0 uses the console's own, which keeps audio in sync")
	flag.IntVar(&c.scale, "scale", 3, "video upscale factor")
	flag.Parse()

	if err := run(c); err != nil {
		log.Fatal(err)
	}
}

func run(c config) error {
	start := time.Now()

	s, err := session.Open(session.Config{CorePath: c.core, ROMPath: c.rom})
	if err != nil {
		return err
	}
	defer s.Close()

	if c.fps <= 0 {
		c.fps = s.FPS()
	}
	track, err := session.ParseBGM(c.music)
	if err != nil {
		return err
	}

	tmp, err := os.MkdirTemp("", "mcpaint-cli-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	var parts []capture.Part

	// The machine starting up, when asked for.
	if c.video != "" && c.titleSeconds > 0 {
		part, err := recordTitle(s, c, tmp)
		if err != nil {
			return err
		}
		parts = append(parts, part)
		fmt.Println("recorded the title screen")
	}

	if err := s.PrepareCanvas(track); err != nil {
		return err
	}
	fmt.Printf("canvas ready in %s, %q at its first note\n",
		time.Since(start).Round(time.Millisecond), c.music)

	ops, err := buildOps(s, c)
	if err != nil {
		return err
	}

	if len(ops) > 0 {
		part, err := recordDrawing(s, c, tmp, ops)
		if err != nil {
			return err
		}
		if part.Video != "" {
			parts = append(parts, part)
		}
	}

	if c.video != "" && len(parts) > 0 {
		// The console fades through black between its own screens, so the
		// seams do the same rather than cutting.
		if err := capture.JoinWith(c.video, parts, capture.JoinOptions{
			FadeSeconds: 0.6, OpenCold: true, EndCold: true, SeamFades: true,
		}); err != nil {
			return err
		}
		fmt.Printf("wrote %s (%.1fs, with sound)\n", c.video, capture.Duration(c.video))
	}

	return writeStill(s, c, start)
}

// buildOps decides what to draw: an operations file, an imported image
// replayed as strokes, or an imported image pasted straight in.
func buildOps(s *session.Session, c config) ([]mp.Op, error) {
	if c.opsFile != "" {
		raw, err := os.ReadFile(c.opsFile)
		if err != nil {
			return nil, err
		}
		var list []mp.Op
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		return list, nil
	}
	if c.image == "" {
		return nil, nil
	}

	f, err := os.Open(c.image)
	if err != nil {
		return nil, err
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		return nil, fmt.Errorf("decoding %s: %w", c.image, err)
	}

	rules, err := mp.ParseColorRules(c.rules)
	if err != nil {
		return nil, err
	}

	target := mp.NewCanvas()
	target.DrawImageWith(src, mp.FitMode(c.fit), mp.QuantizeOptions{
		Dither: c.dither, Vivid: c.vivid, Rules: rules,
	})

	if !c.strokes {
		s.SetCanvas(target)
		s.RunFrames(8)
		return nil, nil
	}

	ops := mp.CanvasToOps(target)
	fmt.Printf("%d strokes to draw\n", len(ops))
	return ops, nil
}

func recordTitle(s *session.Session, c config, tmp string) (capture.Part, error) {
	if err := s.PrepareTitle(); err != nil {
		return capture.Part{}, err
	}
	video := filepath.Join(tmp, "title.mp4")
	audio := filepath.Join(tmp, "title.wav")

	fr := s.Frame()
	v, err := capture.NewVideo(video, fr.Rect.Dx(), fr.Rect.Dy(), c.fps, c.scale)
	if err != nil {
		return capture.Part{}, err
	}
	s.Core().StartAudioCapture()
	for i := 0; i < int(c.titleSeconds*c.fps); i++ {
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

func recordDrawing(s *session.Session, c config, tmp string, ops []mp.Op) (capture.Part, error) {
	opt := session.DrawOptions{
		TargetSeconds: c.target,
		FPS:           c.fps,
		CaptureAudio:  c.video != "" || c.wav != "",
	}

	video := ""
	if c.video != "" {
		video = filepath.Join(tmp, "draw.mp4")
		fr := s.Frame()
		v, err := capture.NewVideo(video, fr.Rect.Dx(), fr.Rect.Dy(), c.fps, c.scale)
		if err != nil {
			return capture.Part{}, err
		}
		opt.Video = v
		opt.HoldFrames = int(c.fps * 2)
	}

	samples, err := s.Draw(ops, opt)
	if opt.Video != nil {
		if cerr := opt.Video.Close(); err == nil {
			err = cerr
		}
	}
	if err != nil {
		return capture.Part{}, err
	}
	fmt.Printf("drew %d operations\n", len(ops))

	if !opt.CaptureAudio {
		return capture.Part{}, nil
	}

	audio := c.wav
	if audio == "" {
		audio = filepath.Join(tmp, "draw.wav")
	}
	if err := capture.WriteWAV(audio, samples, s.SampleRate()); err != nil {
		return capture.Part{}, err
	}
	return capture.Part{Video: video, Audio: audio}, nil
}

func writeStill(s *session.Session, c config, start time.Time) error {
	s.SetCursor(240, 20)
	s.RunFrames(4)

	var img image.Image = s.Canvas().ToImage()
	if c.full {
		img = s.Frame()
	}

	if err := os.MkdirAll(filepath.Dir(c.out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(c.out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}

	b := img.Bounds()
	fmt.Printf("wrote %s (%dx%d) in %s\n",
		c.out, b.Dx(), b.Dy(), time.Since(start).Round(time.Millisecond))
	return nil
}
