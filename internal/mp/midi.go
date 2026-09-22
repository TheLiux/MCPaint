package mp

import (
	"fmt"
	"sort"

	"gitlab.com/gomidi/midi/v2"
	"gitlab.com/gomidi/midi/v2/smf"
)

// staffMIDI lists the MIDI note number of each staff position, 1..13.
// The staff is diatonic: B3, then C4 up to G5, with no accidentals anywhere.
var staffMIDI = [MaxPitch + 1]int{0, 59, 60, 62, 64, 65, 67, 69, 71, 72, 74, 76, 77, 79}

// ImportReport explains what had to give when a MIDI file was squeezed into
// Mario Paint's 96 columns, 13 pitches and three voices.
type ImportReport struct {
	Pages         int      `json:"pages" jsonschema:"staves the piece needed, 96 columns each"`
	NotesRead     int      `json:"notesRead"`
	NotesPlaced   int      `json:"notesPlaced"`
	Transposed    int      `json:"transposed" jsonschema:"notes moved by whole octaves to reach the staff"`
	Snapped       int      `json:"snapped" jsonschema:"sharps and flats pulled to the nearest staff position"`
	DroppedVoices int      `json:"droppedVoices" jsonschema:"notes lost because a column already held three"`
	DroppedLength int      `json:"droppedLength" jsonschema:"notes past the last column of the last page"`
	Warnings      []string `json:"warnings,omitempty"`
}

// MIDIOptions tunes the conversion.
type MIDIOptions struct {
	// StepsPerQuarter is how many columns one quarter note occupies. Higher
	// values keep more rhythmic detail but run out of columns sooner.
	StepsPerQuarter int

	// Transpose shifts every note by this many semitones before fitting.
	Transpose int

	// Instruments maps a MIDI channel (0-15) to a Mario Paint instrument.
	// Channels with no entry fall back to a rotation through the palette.
	Instruments map[int]byte
}

// fitPitch maps a MIDI note number onto a staff position, moving it by whole
// octaves first and only then snapping an accidental to its neighbour.
func fitPitch(note int) (pitch byte, transposed, snapped bool) {
	lo, hi := staffMIDI[MinPitch], staffMIDI[MaxPitch]
	for note < lo {
		note += 12
		transposed = true
	}
	for note > hi {
		note -= 12
		transposed = true
	}

	best, bestDist := byte(MinPitch), 128
	for p := MinPitch; p <= MaxPitch; p++ {
		d := note - staffMIDI[p]
		if d < 0 {
			d = -d
		}
		if d < bestDist {
			best, bestDist = byte(p), d
		}
	}
	return best, transposed, bestDist != 0
}

// midiEvent is one note start, already placed on the column grid.
type midiEvent struct {
	column  int
	note    int
	channel int
	order   int
}

// readMIDI collects note starts and lays them on the column grid.
func readMIDI(path string, opt MIDIOptions) ([]midiEvent, error) {
	steps := opt.StepsPerQuarter
	if steps <= 0 {
		steps = 2 // eighth notes: the usual sweet spot for 96 columns
	}

	tr := smf.ReadTracks(path).Only(midi.NoteOnMsg)
	res := tr.SMF()
	if res == nil {
		return nil, fmt.Errorf("reading %s: not a readable MIDI file", path)
	}
	ticks, ok := res.TimeFormat.(smf.MetricTicks)
	if !ok || ticks.Resolution() == 0 {
		return nil, fmt.Errorf("reading %s: unsupported time format", path)
	}
	perQuarter := int64(ticks.Resolution())

	var events []midiEvent
	n := 0
	tr.Do(func(te smf.TrackEvent) {
		var ch, key, vel uint8
		if !te.Message.GetNoteStart(&ch, &key, &vel) {
			return
		}
		col := int((te.AbsTicks*int64(steps) + perQuarter/2) / perQuarter)
		events = append(events, midiEvent{col, int(key) + opt.Transpose, int(ch), n})
		n++
	})
	if err := tr.Error(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	// Stable order by time, then by pitch descending: when a column overflows
	// the melody on top survives and an inner voice is what gets dropped.
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].column != events[j].column {
			return events[i].column < events[j].column
		}
		if events[i].note != events[j].note {
			return events[i].note > events[j].note
		}
		return events[i].order < events[j].order
	})
	return events, nil
}

// instrumentPicker maps MIDI channels to Mario Paint instruments, falling back
// to a rotation through the palette for channels the caller did not name.
func instrumentPicker(opt MIDIOptions) func(int) byte {
	assigned := map[int]byte{}
	var next byte
	return func(ch int) byte {
		if opt.Instruments != nil {
			if v, ok := opt.Instruments[ch]; ok {
				return v
			}
		}
		if v, ok := assigned[ch]; ok {
			return v
		}
		v := next % byte(len(instrumentNames))
		assigned[ch] = v
		next++
		return v
	}
}

// ImportMIDI converts a Standard MIDI File into a single Mario Paint song,
// dropping anything past the 96th column.
func ImportMIDI(path string, opt MIDIOptions) (*Song, *ImportReport, error) {
	songs, rep, err := ImportMIDIPages(path, opt, 1)
	if err != nil {
		return nil, nil, err
	}
	return songs[0], rep, nil
}

// ImportMIDIPages converts a Standard MIDI File into as many staves as the
// piece needs, 96 columns each.
//
// Mario Paint holds one song at a time, so a piece longer than a staff has to
// be played a page at a time and the recordings stitched together. maxPages
// caps that; pass 0 for no cap.
func ImportMIDIPages(path string, opt MIDIOptions, maxPages int) ([]*Song, *ImportReport, error) {
	events, err := readMIDI(path, opt)
	if err != nil {
		return nil, nil, err
	}

	pagesNeeded := 1
	if len(events) > 0 {
		last := events[len(events)-1].column
		pagesNeeded = last/SongColumns + 1
	}
	pages := pagesNeeded
	if maxPages > 0 && pages > maxPages {
		pages = maxPages
	}

	songs := make([]*Song, pages)
	for i := range songs {
		songs[i] = NewSong()
	}

	rep := &ImportReport{NotesRead: len(events), Pages: pages}
	instrumentFor := instrumentPicker(opt)

	for _, e := range events {
		page := e.column / SongColumns
		if page >= pages {
			rep.DroppedLength++
			continue
		}
		pitch, transposed, snapped := fitPitch(e.note)
		if transposed {
			rep.Transposed++
		}
		if snapped {
			rep.Snapped++
		}
		if songs[page].Add(e.column%SongColumns, pitch, instrumentFor(e.channel)) {
			rep.NotesPlaced++
		} else {
			rep.DroppedVoices++
		}
	}

	if rep.DroppedLength > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d notes fell past page %d; raise the page limit or lower stepsPerQuarter",
			rep.DroppedLength, pages))
	}
	if rep.DroppedVoices > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d notes were dropped because a column already held %d",
			rep.DroppedVoices, SongChannels))
	}
	if rep.Snapped > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d sharps or flats were snapped to the nearest staff position; the staff has none",
			rep.Snapped))
	}
	if pages > 1 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"the piece needed %d staves of %d columns; it plays a page at a time",
			pages, SongColumns))
	}
	return songs, rep, nil
}
