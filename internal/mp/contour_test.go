package mp

import (
	"fmt"
	"os"
	"testing"
)

// TestMelodyContour checks how much of the tune's shape survives the staff.
//
// Being unable to listen, the useful question is whether the melody still
// moves the way it did: if a phrase that rose now falls, the tune is gone
// however many notes were placed.
func TestMelodyContour(t *testing.T) {
	path := os.Getenv("MIDI")
	if path == "" {
		t.Skip("set MIDI")
	}

	opt := MIDIOptions{StepsPerQuarter: 2}
	if v := os.Getenv("TRANSPOSE"); v != "" {
		fmt.Sscanf(v, "%d", &opt.Transpose)
	}

	parts, err := Parts(path, opt)
	if err != nil {
		t.Fatal(err)
	}
	melody := -1
	for _, p := range parts {
		if p.Role == RoleMelody {
			melody = p.Channel
		}
	}
	if melody < 0 {
		t.Skip("no melody channel found")
	}

	events, err := readMIDI(path, opt)
	if err != nil {
		t.Fatal(err)
	}

	var source []int
	var staff []byte
	var last byte
	for _, e := range events {
		if e.channel != melody {
			continue
		}
		p, _, _ := fitPitchNear(e.note, last)
		last = p
		source = append(source, e.note)
		staff = append(staff, p)
	}

	same, moved, flattened := 0, 0, 0
	for i := 1; i < len(source); i++ {
		ds := source[i] - source[i-1]
		dp := int(staff[i]) - int(staff[i-1])
		switch {
		case ds == 0:
			continue
		case dp == 0:
			flattened++
			moved++
		case (ds > 0) == (dp > 0):
			same++
			moved++
		default:
			moved++
		}
	}

	if moved == 0 {
		t.Fatal("the melody never moves")
	}
	kept := float64(same) / float64(moved) * 100
	fmt.Printf("melody on channel %d: %d notes\n", melody, len(source))
	fmt.Printf("  steps in the same direction as the original: %.1f%%\n", kept)
	fmt.Printf("  steps flattened to no movement:              %.1f%%\n",
		float64(flattened)/float64(moved)*100)

	if kept < 70 {
		t.Errorf("only %.0f%% of the melody's movement survived; the tune will not be recognisable", kept)
	}
}
