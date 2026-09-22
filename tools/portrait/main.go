// portrait records a full run: the machine booting to the title screen, then
// the canvas drawing an imported picture stroke by stroke, with music.
package main

import (
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
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
		in     = flag.String("image", "", "picture to redraw")
		out    = flag.String("out", "out/portrait.mp4", "video destination")
		still  = flag.String("still", "out/portrait.png", "final canvas as a PNG")
		fit    = flag.String("fit", "cover", "contain, cover or stretch")
		dither = flag.Bool("dither", true, "Floyd-Steinberg dithering")
		secs   = flag.Float64("seconds", 20, "target length of the drawing")
		title  = flag.Float64("title", 3, "seconds of title screen before the drawing")
		music  = flag.String("music", "theme-1", "canvas track")
	)
	flag.Parse()

	s, err := session.Open(session.Config{
		CorePath: os.Getenv("MCPAINT_CORE"),
		ROMPath:  os.Getenv("MCPAINT_ROM"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()

	f, err := os.Open(*in)
	if err != nil {
		log.Fatal(err)
	}
	src, _, err := image.Decode(f)
	f.Close()
	if err != nil {
		log.Fatal(err)
	}

	// Quantize first, then work out the strokes that would produce it.
	target := mp.NewCanvas()
	target.DrawImage(src, mp.FitMode(*fit), *dither)
	ops := mp.CanvasToOps(target)
	fmt.Printf("%d strokes to draw\n", len(ops))

	os.MkdirAll("out", 0o755)
	tmp, err := os.MkdirTemp("", "portrait-")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	fps := s.FPS()
	var parts []capture.Part

	if *title > 0 {
		if err := s.PrepareTitle(); err != nil {
			log.Fatal(err)
		}
		path := filepath.Join(tmp, "title.mp4")
		wav := filepath.Join(tmp, "title.wav")
		fr := s.Frame()
		v, err := capture.NewVideo(path, fr.Rect.Dx(), fr.Rect.Dy(), fps, 3)
		if err != nil {
			log.Fatal(err)
		}
		s.Core().StartAudioCapture()
		for i := 0; i < int(*title*fps); i++ {
			s.Run()
			if err := v.Write(s.Frame()); err != nil {
				log.Fatal(err)
			}
		}
		sam := s.Core().StopAudioCapture()
		if err := v.Close(); err != nil {
			log.Fatal(err)
		}
		if err := capture.WriteWAV(wav, sam, s.SampleRate()); err != nil {
			log.Fatal(err)
		}
		parts = append(parts, capture.Part{Video: path, Audio: wav})
		fmt.Println("recorded the title screen")
	}

	track, err := session.ParseBGM(*music)
	if err != nil {
		log.Fatal(err)
	}
	if err := s.PrepareCanvas(track); err != nil {
		log.Fatal(err)
	}

	drawPath := filepath.Join(tmp, "draw.mp4")
	drawWav := filepath.Join(tmp, "draw.wav")
	fr := s.Frame()
	v, err := capture.NewVideo(drawPath, fr.Rect.Dx(), fr.Rect.Dy(), fps, 3)
	if err != nil {
		log.Fatal(err)
	}
	samples, err := s.Draw(ops, session.DrawOptions{
		TargetSeconds: *secs,
		FPS:           fps,
		Video:         v,
		HoldFrames:    int(fps * 2.5),
		CaptureAudio:  true,
	})
	if err != nil {
		log.Fatal(err)
	}
	if err := v.Close(); err != nil {
		log.Fatal(err)
	}
	if err := capture.WriteWAV(drawWav, samples, s.SampleRate()); err != nil {
		log.Fatal(err)
	}
	parts = append(parts, capture.Part{Video: drawPath, Audio: drawWav})

	if err := capture.Join(*out, parts); err != nil {
		log.Fatal(err)
	}

	fh, err := os.Create(*still)
	if err != nil {
		log.Fatal(err)
	}
	png.Encode(fh, s.Canvas().ToImage())
	fh.Close()

	fmt.Printf("wrote %s (%.1fs) and %s\n", *out, capture.Duration(*out), *still)
}
