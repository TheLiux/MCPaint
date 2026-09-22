package mp

import (
	"os"
	"path/filepath"
	"testing"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

// writeTestMIDI builds a small file: a C major scale plus one chord and one
// note far outside the staff, so every fitting path is exercised.
func writeTestMIDI(t *testing.T) string {
	t.Helper()

	var s smf.SMF
	s.TimeFormat = smf.MetricTicks(960)
	var tr smf.Track

	add := func(delta uint32, key uint8) {
		tr.Add(delta, midi.NoteOn(0, key, 100))
		tr.Add(480, midi.NoteOff(0, key))
	}
	for _, k := range []uint8{60, 62, 64, 65, 67, 69, 71, 72} { // C4..C5
		add(0, k)
	}
	tr.Add(0, midi.NoteOn(0, 61, 100)) // C#4: must be snapped
	tr.Add(0, midi.NoteOn(0, 64, 100))
	tr.Add(0, midi.NoteOn(0, 67, 100))
	tr.Add(0, midi.NoteOn(0, 24, 100)) // far below the staff: must transpose
	tr.Add(480, midi.NoteOff(0, 61))
	tr.Close(0)
	s.Add(tr)

	path := filepath.Join(t.TempDir(), "test.mid")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return path
}

func TestImportMIDIFitsTheStaff(t *testing.T) {
	song, rep, err := ImportMIDI(writeTestMIDI(t), MIDIOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.NotesRead == 0 {
		t.Fatal("read no notes")
	}
	if rep.NotesPlaced == 0 {
		t.Fatal("placed no notes")
	}
	if rep.Snapped == 0 {
		t.Error("the C sharp should have been snapped to a staff position")
	}
	if rep.Transposed == 0 {
		t.Error("the note two octaves below the staff should have been transposed")
	}

	for _, n := range song.Notes {
		if n.Pitch < MinPitch || n.Pitch > MaxPitch {
			t.Errorf("note out of range: %+v", n)
		}
		if n.Column < 0 || n.Column >= SongColumns {
			t.Errorf("column out of range: %+v", n)
		}
		if n.Channel < 0 || n.Channel >= SongChannels {
			t.Errorf("channel out of range: %+v", n)
		}
	}
}

func TestFitPitchSnapsAndTransposes(t *testing.T) {
	for _, c := range []struct {
		midiNote       int
		want           byte
		wantTransposed bool
		wantSnapped    bool
	}{
		{59, 1, false, false},  // B3, the bottom of the staff
		{79, 13, false, false}, // G5, the top
		{60, 2, false, false},  // C4
		{61, 2, false, true},   // C#4 snaps down to C4
		{24, 2, true, false},   // C1 climbs three octaves to C4
		{96, 9, true, false},   // C7 drops by octaves to C5, the nearest C on the staff
	} {
		got, tr, sn := fitPitch(c.midiNote)
		if got != c.want || tr != c.wantTransposed || sn != c.wantSnapped {
			t.Errorf("fitPitch(%d) = %d,%v,%v; want %d,%v,%v",
				c.midiNote, got, tr, sn, c.want, c.wantTransposed, c.wantSnapped)
		}
	}
}
