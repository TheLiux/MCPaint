package session

import (
	"fmt"
	"strings"

	"github.com/TheLiux/MCPaint/internal/retro"

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
//
// STOP is pressed first to rewind. Without it a second playback is silent:
// the playhead is still parked at the end of the previous run, and PLAY
// resumes from there rather than starting over.
func (s *Session) Play(opt PlayOptions) ([]int16, error) {
	frames := opt.Frames
	if frames <= 0 {
		frames = 600
	}

	s.Click(ComposerStopX, ComposerStopY)
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

// BGM selects what plays on the canvas screen while you draw.
type BGM int

const (
	BGMTheme1   BGM = iota // the default canvas tune
	BGMTheme2              // the second canvas tune
	BGMYourSong            // whatever is currently in the composer
	BGMOff                 // silence
)

// bgmRowY is the screen y of each switch on the SELECT MUSIC list.
var bgmRowY = [...]int{75, 107, 138, 172}

// ParseBGM accepts a track name or an index 0..3.
func ParseBGM(s string) (BGM, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "theme-1", "theme1", "0", "":
		return BGMTheme1, nil
	case "theme-2", "theme2", "1":
		return BGMTheme2, nil
	case "your-song", "song", "composition", "2":
		return BGMYourSong, nil
	case "off", "silence", "none", "3":
		return BGMOff, nil
	}
	return 0, fmt.Errorf("unknown music track %q (want theme-1, theme-2, your-song or off)", s)
}

// BGMName returns the track name for a BGM value.
func BGMName(b BGM) string {
	switch b {
	case BGMTheme1:
		return "theme-1"
	case BGMTheme2:
		return "theme-2"
	case BGMYourSong:
		return "your-song"
	case BGMOff:
		return "off"
	}
	return ""
}

// SelectBGM picks a canvas track and returns to the canvas.
func (s *Session) SelectBGM(track BGM) {
	if int(track) < 0 || int(track) >= len(bgmRowY) {
		return
	}
	s.openBGMScreen()
	s.Click(bgmSwitchX, bgmRowY[track])
	s.RunFrames(60)
	s.Click(toolbarExitX, toolbarY)
	s.RunFrames(90)
}

func (s *Session) openBGMScreen() {
	s.Click(toolbarNextX, toolbarY)
	s.Click(toolbarBGMX, toolbarY)
	s.RunFrames(90)
}

// RestartBGM leaves the game on the canvas with the chosen track starting from
// its first note.
//
// Simply re-picking a track does not rewind it -- the sequencer keeps running
// underneath. Switching to silence stops it, and switching back starts the
// tune again from the top, about 36 frames later. Leaving the screen takes
// roughly 32, so the canvas is back up a few frames before the music begins.
func (s *Session) RestartBGM(track BGM) {
	if int(track) < 0 || int(track) >= len(bgmRowY) {
		return
	}
	s.openBGMScreen()

	s.Click(bgmSwitchX, bgmRowY[BGMOff])
	s.RunFrames(120)
	if track == BGMOff {
		s.Click(toolbarExitX, toolbarY)
		s.RunFrames(90)
		return
	}

	// From here on speed matters, so the clicks are kept short.
	s.quickClick(bgmSwitchX, bgmRowY[track])
	s.quickClick(toolbarExitX, toolbarY)

	// Stop as soon as the canvas is showing rather than counting frames, so
	// this keeps working if the transition takes a different number of them.
	for i := 0; i < 120; i++ {
		s.core.Run()
		if s.onCanvas() {
			return
		}
	}
}

// quickClick is Click with the settling frames trimmed away.
func (s *Session) quickClick(x, y int) {
	for i := 0; i < 2; i++ {
		s.SetCursor(x, y)
		s.core.SetMouse(retro.MouseState{})
		s.core.Run()
	}
	for i := 0; i < 4; i++ {
		s.SetCursor(x, y)
		s.core.SetMouse(retro.MouseState{Left: true})
		s.core.Run()
	}
	s.core.SetMouse(retro.MouseState{})
}
