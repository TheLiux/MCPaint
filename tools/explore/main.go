// Explore: does a second playback need a rewind first?
package main

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
)

func rms(s []int16) float64 {
	if len(s) == 0 {
		return 0
	}
	var sum float64
	for _, v := range s {
		sum += float64(v) * float64(v)
	}
	return math.Sqrt(sum / float64(len(s)))
}

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

	scale := func() *mp.Song {
		song := mp.NewSong()
		for col := 0; col < 12; col++ {
			song.Add(col, byte(col%13)+1, 0)
		}
		song.Tempo = 0x30
		return song
	}

	s.SetSong(scale())
	s.RunFrames(10)
	a, _ := s.Play(session.PlayOptions{Frames: 240})
	fmt.Printf("play 1:              rms=%.0f\n", rms(a))

	s.SetSong(scale())
	s.RunFrames(10)
	b, _ := s.Play(session.PlayOptions{Frames: 240})
	fmt.Printf("play 2 (no rewind):  rms=%.0f\n", rms(b))

	// Try a STOP press before playing again.
	s.Click(session.ComposerStopX, session.ComposerStopY)
	s.RunFrames(20)
	c, _ := s.Play(session.PlayOptions{Frames: 240})
	fmt.Printf("play 3 (STOP first): rms=%.0f\n", rms(c))

	// Try dragging the scroll bar back to the start.
	s.Click(session.ComposerStopX, session.ComposerStopY)
	s.RunFrames(10)
	for i := 0; i < 12; i++ {
		s.Click(196, 152) // left arrow of the scroll bar
	}
	d, _ := s.Play(session.PlayOptions{Frames: 240})
	fmt.Printf("play 4 (scroll home): rms=%.0f\n", rms(d))
}
