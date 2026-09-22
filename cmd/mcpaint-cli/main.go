// Command mcpaint-cli is a development harness for driving Mario Paint directly,
// without going through the MCP server.
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

func main() {
	var (
		core   = flag.String("core", os.Getenv("MCPAINT_CORE"), "libretro core")
		rom    = flag.String("rom", os.Getenv("MCPAINT_ROM"), "Mario Paint ROM")
		in     = flag.String("image", "", "image to draw on the canvas")
		out    = flag.String("out", "out/canvas.png", "screenshot destination")
		fit    = flag.String("fit", "contain", "contain | cover | stretch")
		dither = flag.Bool("dither", true, "Floyd-Steinberg dithering")
		full   = flag.Bool("full", false, "capture the whole screen, not just the canvas")
		ops    = flag.String("ops", "", "JSON file of drawing operations")
		video  = flag.String("video", "", "record the drawing to this MP4")
		secs   = flag.Float64("seconds", 8, "target video length")
		fps    = flag.Float64("fps", 0, "video frame rate; 0 uses the console's own, which keeps audio in sync")
		scale  = flag.Int("scale", 3, "video upscale factor")
		music  = flag.String("music", "theme-1", "canvas track: theme-1, theme-2, your-song or off")
	)
	flag.Parse()

	var drawAudio []int16

	start := time.Now()
	s, err := session.Open(session.Config{CorePath: *core, ROMPath: *rom})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	if *fps <= 0 {
		*fps = s.FPS()
	}

	track, err := session.ParseBGM(*music)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.PrepareCanvas(track); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("canvas ready in %s, music %q at its first note\n",
		time.Since(start).Round(time.Millisecond), *music)

	if *in != "" {
		f, err := os.Open(*in)
		if err != nil {
			log.Fatal(err)
		}
		src, _, err := image.Decode(f)
		f.Close()
		if err != nil {
			log.Fatal(err)
		}

		c := mp.NewCanvas()
		c.DrawImage(src, mp.FitMode(*fit), *dither)
		s.SetCanvas(c)
		s.RunFrames(8)
	}

	if *ops != "" {
		raw, err := os.ReadFile(*ops)
		if err != nil {
			log.Fatal(err)
		}
		var list []mp.Op
		if err := json.Unmarshal(raw, &list); err != nil {
			log.Fatal(err)
		}

		opt := session.DrawOptions{TargetSeconds: *secs, FPS: *fps, HoldFrames: int(*fps * 1.5)}
		opt.CaptureAudio = *video != ""
		if *video != "" {
			f := s.Frame()
			v, err := capture.NewVideo(*video, f.Rect.Dx(), f.Rect.Dy(), *fps, *scale)
			if err != nil {
				log.Fatal(err)
			}
			opt.Video = v
			defer func() {
				if err := v.Close(); err != nil {
					log.Fatal(err)
				}
				silent := filepath.Join(os.TempDir(), "mp-draw-silent.mp4")
				wav := filepath.Join(os.TempDir(), "mp-draw.wav")
				os.Rename(*video, silent)
				if err := capture.WriteWAV(wav, drawAudio, s.SampleRate()); err != nil {
					log.Fatal(err)
				}
				if err := capture.Mux(*video, silent, wav); err != nil {
					log.Fatal(err)
				}
				os.Remove(silent)
				os.Remove(wav)
				fmt.Printf("wrote %s (with sound)\n", *video)
			}()
		}
		samples, err := s.Draw(list, opt)
		if err != nil {
			log.Fatal(err)
		}
		drawAudio = samples
		fmt.Printf("drew %d operations\n", len(list))
	}

	// Park the cursor out of the way of the shot.
	s.SetCursor(240, 20)
	s.RunFrames(4)

	var img image.Image
	if *full {
		img = s.Frame()
	} else {
		img = s.Canvas().ToImage()
	}

	os.MkdirAll("out", 0o755)
	fh, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	defer fh.Close()
	if err := png.Encode(fh, img); err != nil {
		log.Fatal(err)
	}
	b := img.Bounds()
	fmt.Printf("wrote %s (%dx%d) in %s\n", *out, b.Dx(), b.Dy(), time.Since(start).Round(time.Millisecond))
}
