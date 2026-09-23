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
	Pages             int      `json:"pages" jsonschema:"staves the piece needed, 96 columns each"`
	NotesRead         int      `json:"notesRead"`
	NotesPlaced       int      `json:"notesPlaced"`
	Doubled           int      `json:"doubled" jsonschema:"octave doublings that collapsed onto a note already in the column"`
	Spread            int      `json:"spread" jsonschema:"notes moved to the next column because the chord was too thick for three voices"`
	DroppedPercussion int      `json:"droppedPercussion" jsonschema:"drum hits left out; set an instrument for them to keep the pattern"`
	Transposed        int      `json:"transposed" jsonschema:"notes moved by whole octaves to reach the staff"`
	Snapped           int      `json:"snapped" jsonschema:"sharps and flats pulled to the nearest staff position"`
	DroppedVoices     int      `json:"droppedVoices" jsonschema:"notes lost because a column already held three"`
	DroppedLength     int      `json:"droppedLength" jsonschema:"notes past the last column of the last page"`
	Warnings          []string `json:"warnings,omitempty"`
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

	// Percussion names the instrument to play the drum channel with. General
	// MIDI reserves channel 10 -- index 9 -- for percussion, where the key
	// number picks a drum rather than a pitch, so reading those as notes puts
	// nonsense on the staff. Leave it nil to drop the drums; set it and the
	// pattern is kept, laid on three staff positions standing for low, middle
	// and high drums.
	Percussion *byte

	// PercussionChannel overrides which channel carries the drums. Zero means
	// the General MIDI default.
	PercussionChannel int

	// Channels, when set, keeps only these MIDI channels. A dense
	// arrangement has far more parts than three voices can hold, and picking
	// which ones matter beats letting an arbitrary rule decide.
	Channels []int
}

// percussionChannel returns the channel the drums are on.
func (o MIDIOptions) percussionChannel() int {
	if o.PercussionChannel > 0 {
		return o.PercussionChannel
	}
	return 9
}

// drumPitch places a General MIDI drum on one of three staff positions,
// standing for a low, middle or high drum. The exact pitches are arbitrary;
// what carries is the rhythm and the separation between them.
func drumPitch(key int) byte {
	switch {
	case key <= 41: // kick and low toms
		return 2
	case key <= 50: // snare and mid toms
		return 6
	default: // hats, cymbals and the rest
		return 11
	}
}

// fitPitch maps a MIDI note number onto a staff position, moving it by whole
// octaves first and only then snapping an accidental to its neighbour.
func fitPitch(note int) (pitch byte, transposed, snapped bool) {
	return fitPitchNear(note, 0)
}

// fitPitchNear is fitPitch with a preference for staying near where the part
// already is.
//
// Folding each note independently lets a part land in whichever octave the
// arithmetic happens to reach, so a rising line can jump down an octave
// mid-phrase and a bass can end up on top of the melody. Choosing the octave
// closest to the previous note of the same part keeps the shape of the line.
// Pass 0 for near when the part has not started yet.
func fitPitchNear(note int, near byte) (pitch byte, transposed, snapped bool) {
	lo, hi := staffMIDI[MinPitch], staffMIDI[MaxPitch]

	// Every octave of this note that the staff can hold.
	var candidates []int
	for n := note; n >= lo-11; n -= 12 {
		if n <= hi+11 {
			candidates = append(candidates, n)
		}
	}
	for n := note + 12; n <= hi+11; n += 12 {
		candidates = append(candidates, n)
	}

	best, bestScore, bestSnap, bestShift := byte(0), 1<<30, false, 0
	for _, n := range candidates {
		clamped := n
		for clamped < lo {
			clamped += 12
		}
		for clamped > hi {
			clamped -= 12
		}

		p, dist := byte(MinPitch), 128
		for i := MinPitch; i <= MaxPitch; i++ {
			d := clamped - staffMIDI[i]
			if d < 0 {
				d = -d
			}
			if d < dist {
				p, dist = byte(i), d
			}
		}

		// With somewhere to be near, follow the line. Without -- the first
		// note of a part -- move as few octaves as possible, so a note that
		// already fits stays exactly where it was written.
		shift := (clamped - note) / 12

		var score int
		if near == 0 {
			moved := shift
			if moved < 0 {
				moved = -moved
			}
			score = moved*100 + dist
		} else {
			d := int(p) - int(near)
			if d < 0 {
				d = -d
			}
			score = d*2 + dist
			// Following the line is for notes that have to move anyway. One
			// that fits where it was written stays there, or every leap wider
			// than half an octave gets folded back into a step.
			if shift != 0 {
				score += 1000
			}
		}
		if best == 0 || score < bestScore {
			best, bestScore, bestSnap, bestShift = p, score, dist != 0, shift
		}
	}

	// Transposed means moved by whole octaves. A note pulled to its neighbour
	// because the staff has no accidentals was snapped, not transposed.
	return best, bestShift != 0, bestSnap
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

	if len(opt.Channels) > 0 {
		keep := map[int]bool{}
		for _, ch := range opt.Channels {
			keep[ch] = true
		}
		filtered := events[:0]
		for _, e := range events {
			if keep[e.channel] {
				filtered = append(filtered, e)
			}
		}
		events = filtered
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

// assignInstruments gives each channel an instrument suited to its register.
//
// Channels are ranked by their median pitch and handed instruments spanning
// the palette from darkest to brightest, so a bass line does not end up on the
// same bright voice as the melody. Anything the caller named explicitly wins.
func assignInstruments(events []midiEvent, opt MIDIOptions) map[int]byte {
	drums := opt.percussionChannel()

	pitches := map[int][]int{}
	for _, e := range events {
		if e.channel == drums {
			continue
		}
		pitches[e.channel] = append(pitches[e.channel], e.note)
	}

	channels := make([]int, 0, len(pitches))
	for ch := range pitches {
		channels = append(channels, ch)
	}
	sort.Slice(channels, func(i, j int) bool {
		a, b := pitches[channels[i]], pitches[channels[j]]
		sort.Ints(a)
		sort.Ints(b)
		return a[len(a)/2] < b[len(b)/2]
	})

	var taken []byte
	if opt.Percussion != nil {
		taken = append(taken, *opt.Percussion)
	}
	palette := spreadInstruments(len(channels), taken...)
	out := map[int]byte{}
	for i, ch := range channels {
		out[ch] = palette[i]
	}
	for ch, v := range opt.Instruments {
		out[ch] = v
	}
	return out
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

// ChannelSummary describes one channel of a MIDI file and what it was given.
type ChannelSummary struct {
	Channel    int    `json:"channel"`
	Notes      int    `json:"notes"`
	Low        int    `json:"low"`
	High       int    `json:"high"`
	Instrument string `json:"instrument"`
	Percussion bool   `json:"percussion"`
}

// Channels reports what a MIDI file contains and how it would be voiced.
func Channels(path string, opt MIDIOptions) ([]ChannelSummary, error) {
	events, err := readMIDI(path, opt)
	if err != nil {
		return nil, err
	}
	instruments := assignInstruments(events, opt)
	drums := opt.percussionChannel()

	byChan := map[int]*ChannelSummary{}
	for _, e := range events {
		c := byChan[e.channel]
		if c == nil {
			c = &ChannelSummary{Channel: e.channel, Low: 127, Percussion: e.channel == drums}
			byChan[e.channel] = c
		}
		c.Notes++
		note := e.note - opt.Transpose
		if note < c.Low {
			c.Low = note
		}
		if note > c.High {
			c.High = note
		}
	}

	out := make([]ChannelSummary, 0, len(byChan))
	for _, c := range byChan {
		switch {
		case c.Percussion && opt.Percussion != nil:
			c.Instrument = InstrumentName(*opt.Percussion)
		case c.Percussion:
			c.Instrument = "(dropped)"
		default:
			c.Instrument = InstrumentName(instruments[c.Channel])
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Notes > out[j].Notes })
	return out, nil
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
	instruments := assignInstruments(events, opt)

	// Fold every note onto the staff first, keeping each part near itself.
	type placed struct {
		column int
		pitch  byte
		instr  byte
	}
	lastOf := map[int]byte{}
	byColumn := map[int][]placed{}

	drums := opt.percussionChannel()

	for _, e := range events {
		if e.channel == drums {
			if opt.Percussion == nil {
				rep.DroppedPercussion++
				continue
			}
			if e.column/SongColumns >= pages {
				rep.DroppedLength++
				continue
			}
			byColumn[e.column] = append(byColumn[e.column],
				placed{e.column, drumPitch(e.note - opt.Transpose), *opt.Percussion})
			continue
		}

		pitch, transposed, snapped := fitPitchNear(e.note, lastOf[e.channel])
		if transposed {
			rep.Transposed++
		}
		if snapped {
			rep.Snapped++
		}
		lastOf[e.channel] = pitch

		if e.column/SongColumns >= pages {
			rep.DroppedLength++
			continue
		}
		byColumn[e.column] = append(byColumn[e.column],
			placed{e.column, pitch, instruments[e.channel]})
	}

	columns := make([]int, 0, len(byColumn))
	for col := range byColumn {
		columns = append(columns, col)
	}
	sort.Ints(columns)

	for _, col := range columns {
		notes := byColumn[col]

		// Octave doublings collapse onto the same position once the staff has
		// folded them, so a chord can arrive holding the same note three
		// times. Dropping the repeats is free: they would have sounded as a
		// unison while using up the voices a real chord tone needed.
		seen := map[byte]bool{}
		unique := notes[:0]
		for _, n := range notes {
			if seen[n.pitch] {
				rep.Doubled++
				continue
			}
			seen[n.pitch] = true
			unique = append(unique, n)
		}
		notes = unique

		// Still too many: keep the bass and the melody, which carry the
		// harmony's outline, and fill the last voice from the middle where
		// the chord's character lives.
		var spill []placed
		if len(notes) > SongChannels {
			sort.Slice(notes, func(i, j int) bool { return notes[i].pitch < notes[j].pitch })
			bass, melody := notes[0], notes[len(notes)-1]
			middle := notes[len(notes)/2]
			for i, n := range notes {
				if i == 0 || i == len(notes)-1 || i == len(notes)/2 {
					continue
				}
				spill = append(spill, n)
			}
			notes = []placed{bass, middle, melody}
		}

		for _, n := range notes {
			page, within := n.column/SongColumns, n.column%SongColumns
			if songs[page].Add(within, n.pitch, n.instr) {
				rep.NotesPlaced++
			} else {
				rep.DroppedVoices++
			}
		}

		// What would not fit goes to the next column, which turns a chord too
		// thick for three voices into a quick spread of it. That is what a
		// player would do by hand, and it beats losing the note.
		for _, n := range spill {
			next := n.column + 1
			if next/SongColumns != n.column/SongColumns || next >= len(songs)*SongColumns {
				rep.DroppedVoices++
				continue
			}
			page, within := next/SongColumns, next%SongColumns
			if songs[page].Add(within, n.pitch, n.instr) {
				rep.Spread++
				rep.NotesPlaced++
			} else {
				rep.DroppedVoices++
			}
		}
	}

	if rep.DroppedLength > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d notes fell past page %d; raise the page limit or lower stepsPerQuarter",
			rep.DroppedLength, pages))
	}
	if rep.DroppedPercussion > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d drum hits were left out; name a percussion instrument to keep the pattern",
			rep.DroppedPercussion))
	}
	if rep.Doubled > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d octave doublings collapsed onto notes already in their column", rep.Doubled))
	}
	if rep.Spread > 0 {
		rep.Warnings = append(rep.Warnings, fmt.Sprintf(
			"%d notes were spread onto the next column, the chord being too thick for %d voices",
			rep.Spread, SongChannels))
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
