package session

import (
	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
)

// SetSong writes a song into WRAM. As with the canvas, the game picks the
// change up on its own: the staff redraws on the next frame.
func (s *Session) SetSong(song *mp.Song) {
	song.Encode(s.wram[mp.SongBase : mp.SongBase+mp.SongBytes])
	song.WriteSettings(s.wram)
}

// ReadSong reads the song currently in WRAM.
func (s *Session) ReadSong() *mp.Song {
	song := mp.DecodeSong(s.wram[mp.SongBase : mp.SongBase+mp.SongBytes])
	song.Tempo = s.wram[mp.SongTempo]
	song.TimeSignature = s.wram[mp.SongTimeSignature]
	song.Loop = s.wram[mp.SongLoop] != 0
	return song
}

// PlayOptions controls a recorded playback.
type PlayOptions struct {
	Frames int            // how long to record
	Video  *capture.Video // optional, receives the composer screen while it plays
}

// Play presses PLAY and records what comes out.
//
// The audio is whatever the SPC700 actually produces, captured from the same
// run that produced the frames, so picture and sound need no resynchronising.
func (s *Session) Play(opt PlayOptions) ([]int16, error) {
	frames := opt.Frames
	if frames <= 0 {
		frames = 600
	}

	s.Click(ComposerPlayX, ComposerPlayY)
	s.core.StartAudioCapture()

	var err error
	for i := 0; i < frames; i++ {
		s.core.Run()
		if opt.Video != nil && err == nil {
			err = opt.Video.Write(s.core.Frame())
		}
	}
	samples := s.core.StopAudioCapture()
	s.Click(ComposerStopX, ComposerStopY)
	return samples, err
}

// SampleRate is the rate the core reports for its audio.
func (s *Session) SampleRate() int { return int(s.core.AVInfo().SampleRate) }

// FPS is the frame rate the core reports.
func (s *Session) FPS() float64 { return s.core.AVInfo().FPS }
