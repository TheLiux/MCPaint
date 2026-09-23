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

func TestImportMIDIPagesSplitsALongPiece(t *testing.T) {
	path := writeLongMIDI(t)

	// One page has to drop everything past the 96th column.
	one, repOne, err := ImportMIDIPages(path, MIDIOptions{StepsPerQuarter: 2}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(one) != 1 {
		t.Fatalf("asked for one page, got %d", len(one))
	}
	if repOne.DroppedLength == 0 {
		t.Error("a piece longer than a staff should have lost notes off the end")
	}

	// Uncapped, it should spread across pages and keep them.
	all, rep, err := ImportMIDIPages(path, MIDIOptions{StepsPerQuarter: 2}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 2 {
		t.Fatalf("expected more than one page, got %d", len(all))
	}
	if rep.Pages != len(all) {
		t.Errorf("report says %d pages, got %d songs", rep.Pages, len(all))
	}
	if rep.DroppedLength != 0 {
		t.Errorf("%d notes fell off the end with no page limit", rep.DroppedLength)
	}
	if rep.NotesPlaced <= repOne.NotesPlaced {
		t.Errorf("paging placed %d notes, no better than one page's %d",
			rep.NotesPlaced, repOne.NotesPlaced)
	}

	for i, s := range all {
		for _, n := range s.Notes {
			if n.Column < 0 || n.Column >= SongColumns {
				t.Errorf("page %d holds a note at column %d", i, n.Column)
			}
		}
	}
}

// writeLongMIDI builds a piece that needs more than one staff.
func writeLongMIDI(t *testing.T) string {
	t.Helper()

	var s smf.SMF
	s.TimeFormat = smf.MetricTicks(480)
	var tr smf.Track
	for i := 0; i < 140; i++ { // 140 quarters at two columns each
		key := uint8(60 + i%8)
		tr.Add(0, midi.NoteOn(0, key, 100))
		tr.Add(480, midi.NoteOff(0, key))
	}
	tr.Close(0)
	s.Add(tr)

	path := filepath.Join(t.TempDir(), "long.mid")
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

func TestOctaveDoublingsFoldInsteadOfCrowdingAColumn(t *testing.T) {
	// The same note in three octaves collapses onto one staff position, so it
	// should leave room for the rest of the chord rather than fill the column.
	var s smf.SMF
	s.TimeFormat = smf.MetricTicks(480)
	var tr smf.Track
	for _, k := range []uint8{36, 48, 60, 64, 67} { // C2, C3, C4 doubled, plus E and G
		tr.Add(0, midi.NoteOn(0, k, 100))
	}
	tr.Add(480, midi.NoteOff(0, 60))
	tr.Close(0)
	s.Add(tr)

	path := filepath.Join(t.TempDir(), "chord.mid")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.WriteTo(f); err != nil {
		t.Fatal(err)
	}
	f.Close()

	song, rep, err := ImportMIDI(path, MIDIOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Doubled == 0 {
		t.Error("three octaves of C should have folded to one staff position")
	}
	if rep.DroppedVoices != 0 {
		t.Errorf("%d notes dropped; folding the doublings should have left room",
			rep.DroppedVoices)
	}

	// The chord's three distinct tones should all be present.
	pitches := map[byte]bool{}
	for _, n := range song.Notes {
		pitches[n.Pitch] = true
	}
	if len(pitches) != 3 {
		t.Errorf("got %d distinct pitches, want the chord's 3", len(pitches))
	}
}

func TestFitPitchNearFollowsTheLine(t *testing.T) {
	// A note two octaves up should come back down next to where the part is,
	// instead of landing wherever the arithmetic first reaches.
	low, _, _ := fitPitchNear(60, 0) // C4 with no context
	high, _, _ := fitPitchNear(84, low)
	if high != low {
		t.Errorf("C6 next to staff position %d resolved to %d, want %d", low, high, low)
	}
}
