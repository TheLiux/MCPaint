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
		fps    = flag.Float64("fps", 30, "video frame rate")
		scale  = flag.Int("scale", 3, "video upscale factor")
	)
	flag.Parse()

	start := time.Now()
	s, err := session.Open(session.Config{CorePath: *core, ROMPath: *rom})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	if err := s.BootToCanvas(); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("canvas ready in %s\n", time.Since(start).Round(time.Millisecond))

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
				fmt.Printf("wrote %s\n", *video)
			}()
		}
		if err := s.Draw(list, opt); err != nil {
			log.Fatal(err)
		}
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
