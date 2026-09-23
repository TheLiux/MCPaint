package session

import (
	"fmt"
	"math"
	"os"
	"sort"
	"testing"

	"github.com/TheLiux/MCPaint/internal/mp"
)

// TestMeasureTimbres plays every instrument and measures how bright it is and
// how long it rings, so the instrument ordering used elsewhere is based on
// what the chip actually does rather than on the icons.
func TestMeasureTimbres(t *testing.T) {
	core, rom := os.Getenv("MCPAINT_CORE"), os.Getenv("MCPAINT_ROM")
	if core == "" || rom == "" {
		t.Skip("set MCPAINT_CORE and MCPAINT_ROM")
	}

	s, err := Open(Config{CorePath: core, ROMPath: rom})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.OpenComposer(); err != nil {
		t.Fatal(err)
	}

	type result struct {
		name       string
		brightness float64 // spectral centroid, Hz
		sustain    float64 // seconds until it fades
		peak       float64
	}
	var out []result

	for i := byte(0); i < 15; i++ {
		s.Click(ComposerClearX, ComposerClearY)
		s.RunFrames(30)

		song := mp.NewSong()
		song.Tempo = 0x10
		song.Add(0, 7, i) // A4, mid staff
		s.SetSong(song)
		s.RunFrames(10)

		samples, err := s.PlayMeasured(PlayOptions{Frames: int(6 * s.FPS())})
		if err != nil {
			t.Fatal(err)
		}
		if len(samples) < 4096 {
			out = append(out, result{mp.InstrumentName(i), 0, 0, 0})
			continue
		}

		// Mono, and find the peak.
		mono := make([]float64, len(samples)/2)
		peak := 0.0
		for j := range mono {
			v := (float64(samples[j*2]) + float64(samples[j*2+1])) / 2
			mono[j] = v
			if a := math.Abs(v); a > peak {
				peak = a
			}
		}

		rate := float64(s.SampleRate())

		// Spectral centroid over the attack, by direct evaluation at a set of
		// frequencies. A full FFT would be overkill for one number.
		n := 4096
		if len(mono) < n {
			n = len(mono)
		}
		var num, den float64
		for f := 100.0; f < 8000; f *= 1.12 {
			var re, im float64
			w := 2 * math.Pi * f / rate
			for j := 0; j < n; j++ {
				re += mono[j] * math.Cos(w*float64(j))
				im += mono[j] * math.Sin(w*float64(j))
			}
			mag := math.Hypot(re, im)
			num += f * mag
			den += mag
		}
		centroid := 0.0
		if den > 0 {
			centroid = num / den
		}

		// Sustain: where the envelope falls below a tenth of the peak.
		window := int(rate / 50)
		sustain := float64(len(mono)) / rate
		for j := 0; j+window < len(mono); j += window {
			m := 0.0
			for k := j; k < j+window; k++ {
				if a := math.Abs(mono[k]); a > m {
					m = a
				}
			}
			if m < peak*0.1 {
				sustain = float64(j) / rate
				break
			}
		}

		out = append(out, result{mp.InstrumentName(i), centroid, sustain, peak})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].brightness < out[j].brightness })
	fmt.Println("instrument   brightness(Hz)  rings(s)  peak")
	for _, r := range out {
		fmt.Printf("  %-9s   %8.0f      %5.2f   %5.0f\n",
			r.name, r.brightness, r.sustain, r.peak)
	}

	// The ordering baked into the package drives how parts are voiced, so it
	// should still broadly match what the chip does. Compare rank by rank
	// rather than demanding an exact match, since small shifts are expected.
	want := mp.InstrumentsByBrightness()
	rank := map[string]int{}
	for i, v := range want {
		rank[mp.InstrumentName(v)] = i
	}
	inversions := 0
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if rank[out[i].name] > rank[out[j].name] {
				inversions++
			}
		}
	}
	pairs := len(out) * (len(out) - 1) / 2
	if inversions*4 > pairs {
		t.Errorf("measured brightness disagrees with the stored order in %d of %d pairs",
			inversions, pairs)
	}

	// Percussion wants the shortest voice in the palette, so check the one
	// the package names really is among the quickest to fade.
	byRing := append([]result(nil), out...)
	sort.Slice(byRing, func(i, j int) bool { return byRing[i].sustain < byRing[j].sustain })
	drum := mp.InstrumentName(mp.PercussionInstrument)
	place := len(byRing)
	for i, r := range byRing {
		if r.name == drum {
			place = i
			break
		}
	}
	if place > 2 {
		t.Errorf("%s is the %dth shortest voice; something quicker should carry the drums",
			drum, place+1)
	}
}
