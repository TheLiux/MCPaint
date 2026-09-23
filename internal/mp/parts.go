package mp

import "sort"

// Role is what a MIDI channel is doing musically.
type Role string

const (
	RoleMelody     Role = "melody"
	RoleBass       Role = "bass"
	RoleInner      Role = "inner line"
	RoleChords     Role = "chords"
	RolePercussion Role = "percussion"
)

// Part describes one channel of a MIDI file.
type Part struct {
	Channel   int     `json:"channel"`
	Role      Role    `json:"role"`
	Notes     int     `json:"notes"`
	Low       int     `json:"low"`
	High      int     `json:"high"`
	Median    int     `json:"median"`
	Polyphony float64 `json:"polyphony" jsonschema:"average notes sharing an onset; 1.0 is a single line"`
	Density   float64 `json:"density" jsonschema:"notes per quarter note"`
}

// Parts works out what each channel of a MIDI file is doing.
//
// Note count is a poor guide and an actively misleading one: a strummed
// accompaniment can carry six times the notes of the tune it accompanies.
// What separates a melody is that it is a single line -- one note at a time --
// in a high register and at a moderate pace.
func Parts(path string, opt MIDIOptions) ([]Part, error) {
	events, err := readMIDI(path, opt)
	if err != nil {
		return nil, err
	}
	return partsOf(events, opt), nil
}

func partsOf(events []midiEvent, opt MIDIOptions) []Part {
	drums := opt.percussionChannel()

	type acc struct {
		pitches   []int
		onsets    map[int]int
		firstCol  int
		lastCol   int
		seenFirst bool
	}
	byChan := map[int]*acc{}

	for _, e := range events {
		a := byChan[e.channel]
		if a == nil {
			a = &acc{onsets: map[int]int{}, firstCol: e.column}
			byChan[e.channel] = a
		}
		a.pitches = append(a.pitches, e.note)
		a.onsets[e.column]++
		if e.column > a.lastCol {
			a.lastCol = e.column
		}
	}

	steps := opt.StepsPerQuarter
	if steps <= 0 {
		steps = 2
	}

	out := make([]Part, 0, len(byChan))
	for ch, a := range byChan {
		sort.Ints(a.pitches)

		total := 0
		for _, n := range a.onsets {
			total += n
		}
		poly := float64(total) / float64(len(a.onsets))

		quarters := float64(a.lastCol-a.firstCol) / float64(steps)
		density := 0.0
		if quarters > 0 {
			density = float64(len(a.pitches)) / quarters
		}

		p := Part{
			Channel:   ch,
			Notes:     len(a.pitches),
			Low:       a.pitches[0],
			High:      a.pitches[len(a.pitches)-1],
			Median:    a.pitches[len(a.pitches)/2],
			Polyphony: poly,
			Density:   density,
		}

		switch {
		case ch == drums:
			p.Role = RolePercussion
		case poly >= 1.6:
			p.Role = RoleChords
		case p.Median < 48:
			p.Role = RoleBass
		default:
			p.Role = RoleInner
		}
		out = append(out, p)
	}

	// The melody is the single line sitting highest. Ties go to the sparser
	// part, a tune being less busy than a countermelody.
	best, bestScore := -1, -1.0
	for i, p := range out {
		if p.Role != RoleInner || p.Polyphony > 1.25 {
			continue
		}
		score := float64(p.Median) - p.Density
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best >= 0 {
		out[best].Role = RoleMelody
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Median > out[j].Median })
	return out
}

// SelectParts picks the channels worth keeping, at most one per voice.
//
// Three voices go furthest on three single lines -- the tune, a bass under it
// and one inner part -- which is also what leaves the fewest notes fighting
// over a column. Percussion is included only when asked for, since it would
// otherwise spend a third of the polyphony on drums.
func SelectParts(parts []Part, voices int, withDrums bool) []int {
	if voices <= 0 {
		voices = SongChannels
	}
	var chosen []int

	take := func(role Role) bool {
		for _, p := range parts {
			if p.Role != role {
				continue
			}
			for _, c := range chosen {
				if c == p.Channel {
					return false
				}
			}
			chosen = append(chosen, p.Channel)
			return true
		}
		return false
	}

	take(RoleMelody)
	if withDrums {
		take(RolePercussion)
	}
	take(RoleBass)

	// Fill what is left with the remaining single lines, then chords.
	for _, want := range []Role{RoleInner, RoleChords} {
		for _, p := range parts {
			if len(chosen) >= voices {
				break
			}
			if p.Role != want {
				continue
			}
			dup := false
			for _, c := range chosen {
				if c == p.Channel {
					dup = true
				}
			}
			if !dup {
				chosen = append(chosen, p.Channel)
			}
		}
	}
	if len(chosen) > voices {
		chosen = chosen[:voices]
	}
	return chosen
}

// MelodyFit measures how much of a tune's shape survives a given key.
type MelodyFit struct {
	Transpose  int     `json:"transpose"`
	Preserved  float64 `json:"preserved" jsonschema:"percent of melodic steps still moving the same way"`
	Flattened  float64 `json:"flattened" jsonschema:"percent of steps that stopped moving at all"`
	Accidental int     `json:"accidental" jsonschema:"notes pulled to a neighbouring staff position"`
}

// score ranks a fit. Steps that reverse are the worst thing that can happen to
// a tune, and steps that flatten are nearly as bad, so both are charged
// against what survived.
func (f MelodyFit) score() float64 { return f.Preserved - f.Flattened }

// BestKey finds the transposition that keeps most of the melody's shape.
//
// Counting accidentals across every channel is the wrong measure: it is
// dominated by whichever part has the most notes, which is usually the
// accompaniment. What makes a tune recognisable is that its line still rises
// and falls where it used to, so that is what is scored.
func BestKey(path string, opt MIDIOptions) (MelodyFit, error) {
	var best MelodyFit
	found := false

	for shift := -11; shift <= 11; shift++ {
		try := opt
		try.Transpose = shift

		fit, ok, err := melodyFit(path, try)
		if err != nil {
			return best, err
		}
		if !ok {
			continue
		}
		// On a tie the shape is equally safe either way, so the key with
		// fewer accidentals wins, and only then the smaller shift.
		tie := fit.score() == best.score()
		if !found || fit.score() > best.score() ||
			(tie && fit.Accidental < best.Accidental) ||
			(tie && fit.Accidental == best.Accidental && abs(shift) < abs(best.Transpose)) {
			best, found = fit, true
		}
	}
	if !found {
		// No single line to follow, so fall back to fewest accidentals.
		for shift := -11; shift <= 11; shift++ {
			try := opt
			try.Transpose = shift
			_, rep, err := ImportMIDIPages(path, try, 0)
			if err != nil {
				return best, err
			}
			if !found || rep.Snapped < best.Accidental {
				best, found = MelodyFit{Transpose: shift, Accidental: rep.Snapped}, true
			}
		}
	}
	return best, nil
}

// melodyFit walks the melody in one key and reports how it fared.
func melodyFit(path string, opt MIDIOptions) (MelodyFit, bool, error) {
	events, err := readMIDI(path, opt)
	if err != nil {
		return MelodyFit{}, false, err
	}
	parts := partsOf(events, opt)

	melody := -1
	for _, p := range parts {
		if p.Role == RoleMelody {
			melody = p.Channel
		}
	}
	if melody < 0 {
		return MelodyFit{}, false, nil
	}

	fit := MelodyFit{Transpose: opt.Transpose}
	var prevSource int
	var prevStaff, last byte
	started := false
	same, flat, moved := 0, 0, 0

	for _, e := range events {
		if e.channel != melody {
			continue
		}
		p, _, snapped := fitPitchNear(e.note, last)
		last = p
		if snapped {
			fit.Accidental++
		}
		if started {
			ds := e.note - prevSource
			dp := int(p) - int(prevStaff)
			switch {
			case ds == 0:
			case dp == 0:
				flat++
				moved++
			case (ds > 0) == (dp > 0):
				same++
				moved++
			default:
				moved++
			}
		}
		prevSource, prevStaff, started = e.note, p, true
	}

	if moved == 0 {
		return MelodyFit{}, false, nil
	}
	fit.Preserved = float64(same) / float64(moved) * 100
	fit.Flattened = float64(flat) / float64(moved) * 100
	return fit, true, nil
}
