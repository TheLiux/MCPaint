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
