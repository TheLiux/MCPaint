package mp

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// An empty slot in the song data. Both bytes matter: the game writes this
// exact pair for "no note here".
const (
	emptyNote       = 0xFF
	emptyInstrument = 0xDF
)

// Pitch runs from 1 (bottom line of the staff) to 13 (top), thirteen diatonic
// steps. Names were confirmed by placing notes in the composer and reading the
// bytes back.
const (
	MinPitch = 1
	MaxPitch = 13
)

var pitchNames = [MaxPitch + 1]string{
	"", "B3", "C4", "D4", "E4", "F4", "G4", "A4", "B4", "C5", "D5", "E5", "F5", "G5",
}

// Instruments are named after their icons in the composer's palette, in the
// order the palette lays them out. Values 0x00..0x0E.
var instrumentNames = [15]string{
	"mario", "mushroom", "yoshi", "star", "flower",
	"gameboy", "cat", "dog", "pig", "swan",
	"face", "plane", "boat", "car", "heart",
}

// PitchName returns the note name for a pitch, or "" if out of range.
func PitchName(p byte) string {
	if p < MinPitch || p > MaxPitch {
		return ""
	}
	return pitchNames[p]
}

// ParsePitch accepts a note name such as "C4" or a bare number 1..13.
func ParsePitch(s string) (byte, error) {
	t := strings.ToUpper(strings.TrimSpace(s))
	for i := MinPitch; i <= MaxPitch; i++ {
		if pitchNames[i] == t {
			return byte(i), nil
		}
	}
	var n int
	if _, err := fmt.Sscanf(t, "%d", &n); err == nil && n >= MinPitch && n <= MaxPitch {
		return byte(n), nil
	}
	return 0, fmt.Errorf("unknown pitch %q (want %s..%s or 1..13)",
		s, pitchNames[MinPitch], pitchNames[MaxPitch])
}

// InstrumentName returns the palette name for an instrument value.
func InstrumentName(i byte) string {
	if int(i) >= len(instrumentNames) {
		return ""
	}
	return instrumentNames[i]
}

// ParseInstrument accepts an instrument name or a bare number 0..14.
func ParseInstrument(s string) (byte, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	for i, name := range instrumentNames {
		if name == t {
			return byte(i), nil
		}
	}
	var n int
	if _, err := fmt.Sscanf(t, "%d", &n); err == nil && n >= 0 && n < len(instrumentNames) {
		return byte(n), nil
	}
	return 0, fmt.Errorf("unknown instrument %q (want one of %s, or 0..14)",
		s, strings.Join(instrumentNames[:], ", "))
}

// Instruments lists the palette names in value order.
func Instruments() []string { return instrumentNames[:] }

// Note is one placed note.
type Note struct {
	Column     int  `json:"column"`     // 0..95
	Channel    int  `json:"channel"`    // 0..2; a column holds at most three notes
	Pitch      byte `json:"pitch"`      // 1..13
	Instrument byte `json:"instrument"` // 0..14
}

// Time signature values as the game stores them.
const (
	TimeThreeFour = 0
	TimeFourFour  = 1
)

// Song is a Mario Paint composition.
type Song struct {
	Notes         []Note `json:"notes"`
	Tempo         byte   `json:"tempo"` // 1 slowest .. 255 fastest, 0 pauses
	Loop          bool   `json:"loop"`
	TimeSignature byte   `json:"timeSignature"` // 0 = 3/4, 1 = 4/4
}

// NewSong returns an empty song with the game's own defaults.
func NewSong() *Song {
	return &Song{Tempo: 0x12, TimeSignature: TimeFourFour}
}

// Encode writes the song into the 576-byte block the game reads.
func (s *Song) Encode(dst []byte) {
	for i := 0; i < SongBytes; i += 2 {
		dst[i] = emptyNote
		dst[i+1] = emptyInstrument
	}
	for _, n := range s.Notes {
		if n.Column < 0 || n.Column >= SongColumns {
			continue
		}
		if n.Channel < 0 || n.Channel >= SongChannels {
			continue
		}
		if n.Pitch < MinPitch || n.Pitch > MaxPitch {
			continue
		}
		off := n.Column*6 + n.Channel*2
		dst[off] = n.Pitch
		dst[off+1] = n.Instrument
	}
}

// DecodeSong reads notes out of a 576-byte block.
func DecodeSong(src []byte) *Song {
	s := NewSong()
	for col := 0; col < SongColumns; col++ {
		for ch := 0; ch < SongChannels; ch++ {
			off := col*6 + ch*2
			pitch, ins := src[off], src[off+1]
			if pitch == emptyNote && ins == emptyInstrument {
				continue
			}
			if pitch < MinPitch || pitch > MaxPitch {
				continue
			}
			s.Notes = append(s.Notes, Note{col, ch, pitch, ins})
		}
	}
	return s
}

// Add places a note in the first free channel of a column. It reports false
// when the column already holds three notes, which is the game's hard limit.
func (s *Song) Add(column int, pitch, instrument byte) bool {
	used := [SongChannels]bool{}
	for _, n := range s.Notes {
		if n.Column == column {
			used[n.Channel] = true
		}
	}
	for ch := 0; ch < SongChannels; ch++ {
		if !used[ch] {
			s.Notes = append(s.Notes, Note{column, ch, pitch, instrument})
			return true
		}
	}
	return false
}

// WriteSettings stores tempo, loop and time signature into WRAM.
//
// SongScrollEnd is deliberately left alone. Despite its name it is a scroll
// position, not a song length -- it sits at 784 whatever notes are present,
// and writing a column count there cuts playback off after the first note.
func (s *Song) WriteSettings(wram []byte) {
	wram[SongTempo] = s.Tempo
	wram[SongTimeSignature] = s.TimeSignature

	var loop uint16
	if s.Loop {
		loop = 1
	}
	binary.LittleEndian.PutUint16(wram[SongLoop:], loop)
}
