// Package session drives a running Mario Paint: it boots the game to the
// canvas screen, injects content into WRAM, and captures what comes out.
package session

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"image"
	"os"
	"path/filepath"

	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/retro"
)

type Config struct {
	CorePath string
	ROMPath  string
	CacheDir string
}

type Session struct {
	core     *retro.Core
	wram     []byte
	cfg      Config
	stateKey string
}

// Open loads the core and the ROM but does not boot the game.
func Open(cfg Config) (*Session, error) {
	if cfg.CacheDir == "" {
		base, err := os.UserCacheDir()
		if err != nil {
			return nil, err
		}
		cfg.CacheDir = filepath.Join(base, "mcpaint")
	}
	if err := os.MkdirAll(cfg.CacheDir, 0o755); err != nil {
		return nil, err
	}

	rom, err := os.ReadFile(cfg.ROMPath)
	if err != nil {
		return nil, fmt.Errorf("reading ROM: %w", err)
	}

	core, err := retro.Open(cfg.CorePath, cfg.CacheDir, cfg.CacheDir)
	if err != nil {
		return nil, err
	}
	if err := core.LoadGame(cfg.ROMPath, rom); err != nil {
		core.Close()
		return nil, err
	}
	core.SetControllerPortDevice(0, retro.DeviceMouse)

	s := &Session{core: core, cfg: cfg}
	s.wram = core.Memory(retro.MemorySystemRAM)
	if len(s.wram) < 0x20000 {
		core.Close()
		return nil, fmt.Errorf("unexpected WRAM size %d", len(s.wram))
	}
	s.cfg.ROMPath = cfg.ROMPath
	s.stateKey = stateKey(rom)
	return s, nil
}

func (s *Session) Close() { s.core.Close() }

func stateKey(rom []byte) string {
	sum := sha256.Sum256(rom)
	return hex.EncodeToString(sum[:8])
}

// BootToCanvas leaves the game sitting on the canvas screen under our control.
//
// Mario Paint's title screen has no clickable "start": every hotspot there is
// one of the letter gags. The attract demo, however, walks into the canvas on
// its own, so we ride it in and then clear the demo flag to take over. The
// resulting state is cached, because that walk costs ~2400 emulated frames.
func (s *Session) BootToCanvas() error {
	cache := filepath.Join(s.cfg.CacheDir, "canvas-"+s.stateKey+".state")
	if b, err := os.ReadFile(cache); err == nil {
		if err := s.core.Unserialize(b); err == nil {
			s.core.RunFrames(2)
			return nil
		}
		// A stale or mismatched state is not fatal: fall through and re-boot.
	}

	const maxFrames = 8000
	reached := false
	for i := 0; i < maxFrames; i++ {
		s.core.Run()
		if i%10 == 0 && i > 600 && s.onCanvas() {
			reached = true
			break
		}
	}
	if !reached {
		return fmt.Errorf("the attract demo never reached the canvas in %d frames", maxFrames)
	}

	s.wram[mp.DemoFlag] = 0
	s.SetCursor(128, 100)
	s.core.RunFrames(40)

	// Start from a clean sheet: the demo will have drawn on it.
	s.SetCanvas(mp.NewCanvas())
	s.core.RunFrames(4)

	if b, err := s.core.Serialize(); err == nil {
		os.WriteFile(cache, b, 0o644)
	}
	return nil
}

// onCanvas reports whether the canvas screen is showing. The title screen is
// near-monochrome; the canvas carries the full 15-colour palette bar.
func (s *Session) onCanvas() bool {
	f := s.core.Frame()
	if f == nil {
		return false
	}
	seen := map[uint32]struct{}{}
	for i := 0; i+3 < len(f.Pix); i += 4 {
		seen[uint32(f.Pix[i])<<16|uint32(f.Pix[i+1])<<8|uint32(f.Pix[i+2])] = struct{}{}
		if len(seen) > 15 {
			return true
		}
	}
	return false
}

// Canvas reads the current canvas out of WRAM.
func (s *Session) Canvas() *mp.Canvas {
	c := mp.NewCanvas()
	c.Decode(s.wram[mp.CanvasBase : mp.CanvasBase+mp.CanvasBytes])
	return c
}

// SetCanvas writes a canvas into WRAM. The game DMAs the buffer to VRAM every
// frame, so the change shows up on the next frame with no further prompting.
func (s *Session) SetCanvas(c *mp.Canvas) {
	c.Encode(s.wram[mp.CanvasBase : mp.CanvasBase+mp.CanvasBytes])
}

func (s *Session) SetCursor(x, y int) {
	binary.LittleEndian.PutUint16(s.wram[mp.CursorX:], uint16(x))
	binary.LittleEndian.PutUint16(s.wram[mp.CursorY:], uint16(y))
}

func (s *Session) Cursor() (int, int) {
	return int(binary.LittleEndian.Uint16(s.wram[mp.CursorX:])),
		int(binary.LittleEndian.Uint16(s.wram[mp.CursorY:]))
}

func (s *Session) Run()               { s.core.Run() }
func (s *Session) RunFrames(n int)    { s.core.RunFrames(n) }
func (s *Session) Frame() *image.RGBA { return s.core.Frame() }
func (s *Session) Core() *retro.Core  { return s.core }

// Composer navigation. The music tool lives on the second page of the bottom
// toolbar, behind the arrow at the far right.
const (
	toolbarY       = 210
	toolbarNextX   = 232
	toolbarMusicX  = 103 // the composer
	toolbarBGMX    = 151 // SELECT MUSIC, the canvas background track
	toolbarExitX   = 14  // leaves a sub-screen
	bgmSwitchX     = 95  // the switches down the left of the SELECT MUSIC list
	ComposerPlayX  = 58
	ComposerPlayY  = 178
	ComposerStopX  = 28
	ComposerStopY  = 178
	ComposerLoopX  = 92
	ComposerLoopY  = 178
	ComposerClearX = 216
	ComposerClearY = 182
)

// Click presses the left mouse button at a screen position and lets the game
// settle. Coordinates are screen pixels, the same space as the cursor.
func (s *Session) Click(x, y int) {
	for i := 0; i < 3; i++ {
		s.SetCursor(x, y)
		s.core.SetMouse(retro.MouseState{})
		s.core.Run()
	}
	for i := 0; i < 6; i++ {
		s.SetCursor(x, y)
		s.core.SetMouse(retro.MouseState{Left: true})
		s.core.Run()
	}
	for i := 0; i < 12; i++ {
		s.SetCursor(x, y)
		s.core.SetMouse(retro.MouseState{})
		s.core.Run()
	}
}

// OpenComposer leaves the game on the music composer screen.
func (s *Session) OpenComposer() error {
	cache := filepath.Join(s.cfg.CacheDir, "composer-"+s.stateKey+".state")
	if b, err := os.ReadFile(cache); err == nil {
		if err := s.core.Unserialize(b); err == nil {
			s.core.RunFrames(2)
			return nil
		}
	}

	if err := s.BootToCanvas(); err != nil {
		return err
	}
	s.Click(toolbarNextX, toolbarY)
	s.Click(toolbarMusicX, toolbarY)
	s.core.RunFrames(120)

	if b, err := s.core.Serialize(); err == nil {
		os.WriteFile(cache, b, 0o644)
	}
	return nil
}

// Song returns the raw song bytes from WRAM.
func (s *Session) Song() []byte {
	return s.wram[mp.SongBase : mp.SongBase+mp.SongBytes]
}

// WRAM exposes the console's work RAM for tools that need raw access.
func (s *Session) WRAM() []byte { return s.wram }

// PrepareCanvas leaves the game on the canvas with a background track about to
// play its first note, so a recording made from here opens on the downbeat.
//
// The state is cached per track: reaching it means walking through the SELECT
// MUSIC screen twice, which is not free.
func (s *Session) PrepareCanvas(track BGM) error {
	cache := filepath.Join(s.cfg.CacheDir,
		fmt.Sprintf("canvas-%s-%s.state", BGMName(track), s.stateKey))
	if b, err := os.ReadFile(cache); err == nil {
		if err := s.core.Unserialize(b); err == nil {
			s.core.RunFrames(2)
			return nil
		}
	}

	if err := s.BootToCanvas(); err != nil {
		return err
	}
	s.SetCanvas(mp.NewCanvas())
	s.RestartBGM(track)

	if track != BGMOff {
		if err := s.alignToDownbeat(); err != nil {
			return err
		}
	}

	if b, err := s.core.Serialize(); err == nil {
		os.WriteFile(cache, b, 0o644)
	}
	return nil
}

// frameIsLoud reports whether the frame just run produced any sound.
func (s *Session) frameIsLoud() bool {
	s.core.StartAudioCapture()
	s.core.Run()
	for _, v := range s.core.StopAudioCapture() {
		if v > 200 || v < -200 {
			return true
		}
	}
	return false
}

// alignToDownbeat parks the session on the last silent frame before the tune
// starts.
//
// Leaving the SELECT MUSIC screen plays a transition effect, and the tune only
// begins some frames after the silence that follows it. Rather than hard-code
// that gap, this runs ahead to find where the music actually starts and then
// rewinds to just before it, which is what save states make cheap.
func (s *Session) alignToDownbeat() error {
	const maxFrames = 600

	// Let the transition effect finish.
	quiet := 0
	for i := 0; i < maxFrames && quiet < 4; i++ {
		if s.frameIsLoud() {
			quiet = 0
		} else {
			quiet++
		}
	}
	if quiet < 4 {
		return fmt.Errorf("the screen transition never went quiet")
	}

	mark, err := s.core.Serialize()
	if err != nil {
		return err
	}

	steps := 0
	for ; steps < maxFrames; steps++ {
		if s.frameIsLoud() {
			break
		}
	}
	if steps >= maxFrames {
		return fmt.Errorf("the background track never started")
	}

	if err := s.core.Unserialize(mark); err != nil {
		return err
	}
	if steps > 1 {
		s.core.RunFrames(steps - 1)
	}
	return nil
}
