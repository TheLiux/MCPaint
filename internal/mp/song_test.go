package mp

import "testing"

func TestSongRoundTrip(t *testing.T) {
	s := NewSong()
	s.Notes = []Note{
		{Column: 0, Channel: 0, Pitch: 1, Instrument: 0},
		{Column: 0, Channel: 2, Pitch: 13, Instrument: 14},
		{Column: 95, Channel: 1, Pitch: 7, Instrument: 5},
	}

	buf := make([]byte, SongBytes)
	s.Encode(buf)
	got := DecodeSong(buf)

	if len(got.Notes) != len(s.Notes) {
		t.Fatalf("decoded %d notes, want %d", len(got.Notes), len(s.Notes))
	}
	for i, n := range s.Notes {
		if got.Notes[i] != n {
			t.Errorf("note %d: got %+v, want %+v", i, got.Notes[i], n)
		}
	}
}

func TestEncodeMarksEmptySlots(t *testing.T) {
	// The game writes FF DF for an empty slot, and playback depends on it:
	// zeroed bytes are read as a note.
	buf := make([]byte, SongBytes)
	NewSong().Encode(buf)
	for i := 0; i < SongBytes; i += 2 {
		if buf[i] != 0xFF || buf[i+1] != 0xDF {
			t.Fatalf("slot at %d is %02X %02X, want FF DF", i, buf[i], buf[i+1])
		}
	}
}

func TestAddFillsChannelsThenRefuses(t *testing.T) {
	s := NewSong()
	for i := 0; i < SongChannels; i++ {
		if !s.Add(4, 5, 0) {
			t.Fatalf("Add %d rejected while channels were free", i)
		}
	}
	if s.Add(4, 5, 0) {
		t.Error("Add accepted a fourth note in one column")
	}
}

func TestParsePitchAndInstrument(t *testing.T) {
	for _, c := range []struct {
		in   string
		want byte
	}{{"B3", 1}, {"c4", 2}, {"G5", 13}, {"7", 7}} {
		got, err := ParsePitch(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParsePitch(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
	if _, err := ParsePitch("Z9"); err == nil {
		t.Error("ParsePitch accepted a bogus name")
	}

	for _, c := range []struct {
		in   string
		want byte
	}{{"mario", 0}, {"HEART", 14}, {"3", 3}} {
		got, err := ParseInstrument(c.in)
		if err != nil || got != c.want {
			t.Errorf("ParseInstrument(%q) = %d, %v; want %d", c.in, got, err, c.want)
		}
	}
}
