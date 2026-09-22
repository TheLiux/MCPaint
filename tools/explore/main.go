// Explore: does the prepared state really begin with the tune's first note?
package main

import (
	"fmt"
	"log"
	"math"
	"os"

	"github.com/TheLiux/MCPaint/internal/capture"
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
	if err := s.PrepareCanvas(session.BGMTheme1); err != nil {
		log.Fatal(err)
	}
	os.MkdirAll("out", 0o755)

	s.Core().StartAudioCapture()
	var env []float64
	for i := 0; i < 60; i++ {
		s.RunFrames(3)
		sam := s.Core().StopAudioCapture()
		var sum float64
		for _, v := range sam {
			sum += float64(v) * float64(v)
		}
		r := 0.0
		if len(sam) > 0 {
			r = math.Sqrt(sum / float64(len(sam)))
		}
		env = append(env, r)
		s.Core().StartAudioCapture()
	}
	all := s.Core().StopAudioCapture()
	_ = all

	fmt.Println("first three seconds from the prepared state, one bucket per 3 frames:")
	for i, v := range env {
		if i%20 == 0 {
			fmt.Printf("\n  %4.1fs ", float64(i*3)/60)
		}
		fmt.Printf("%4.0f ", v)
	}
	fmt.Println()

	// And a clean recording straight from the state, to listen to.
	if err := s.PrepareCanvas(session.BGMTheme1); err != nil {
		log.Fatal(err)
	}
	s.Core().StartAudioCapture()
	s.RunFrames(480)
	capture.WriteWAV("out/bgm_start.wav", s.Core().StopAudioCapture(), s.SampleRate())
	fmt.Println("wrote out/bgm_start.wav")
}
