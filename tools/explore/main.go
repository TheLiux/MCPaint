// Explore: full music pass — inject, play, record.
package main

import (
	"fmt"
	"image/png"
	"log"
	"math"
	"os"

	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
)

func main() {
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

	song := mp.NewSong()
	for col := 0; col < 32; col++ {
		song.Add(col, byte(col%13)+1, 0)
		if col%4 == 0 {
			song.Add(col, byte((col+4)%13)+1, 3)
		}
	}
	song.Tempo = 0x28
	s.SetSong(song)
	s.RunFrames(20)

	v, _ := capture.NewVideo("out/m3_song.mp4", 256, 224, s.FPS(), 3)
	samples, err := s.Play(session.PlayOptions{Frames: 600, Video: v})
	if err != nil {
		log.Fatal(err)
	}
	v.Close()
	fh, _ := os.Create("out/m3_after.png")
	png.Encode(fh, s.Frame())
	fh.Close()

	capture.WriteWAV("out/m3_song.wav", samples, s.SampleRate())

	// Report loudness per second so a silent tail is obvious.
	per := s.SampleRate() * 2
	fmt.Printf("%d notes, %.2fs audio\n", len(song.Notes), float64(len(samples)/2)/float64(s.SampleRate()))
	for i := 0; i*per < len(samples); i++ {
		w := samples[i*per:]
		if len(w) > per {
			w = w[:per]
		}
		var sum float64
		for _, x := range w {
			sum += float64(x) * float64(x)
		}
		fmt.Printf("  s%-2d rms=%.0f\n", i, math.Sqrt(sum/float64(len(w))))
	}
}
