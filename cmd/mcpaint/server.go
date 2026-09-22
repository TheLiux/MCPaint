package main

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/TheLiux/MCPaint/internal/mp"
	"github.com/TheLiux/MCPaint/internal/session"
)

// screen tracks which part of the game the session is currently sitting on,
// so tools can switch only when they have to -- each switch costs frames.
type screen int

const (
	screenNone screen = iota
	screenCanvas
	screenComposer
)

type server struct {
	mu      sync.Mutex
	cfg     session.Config
	outDir  string
	sess    *session.Session
	current screen
}

func newServer(cfg session.Config, outDir string) *server {
	return &server{cfg: cfg, outDir: outDir}
}

// open boots the emulator on first use. Booting is deferred so the server
// starts instantly and a missing ROM surfaces as a tool error rather than a
// crash at startup.
func (s *server) open() (*session.Session, error) {
	if s.sess != nil {
		return s.sess, nil
	}
	sess, err := session.Open(s.cfg)
	if err != nil {
		return nil, err
	}
	s.sess = sess
	return sess, nil
}

func (s *server) canvas() (*session.Session, error) {
	sess, err := s.open()
	if err != nil {
		return nil, err
	}
	if s.current != screenCanvas {
		if err := sess.BootToCanvas(); err != nil {
			return nil, err
		}
		s.current = screenCanvas
	}
	return sess, nil
}

func (s *server) composer() (*session.Session, error) {
	sess, err := s.open()
	if err != nil {
		return nil, err
	}
	if s.current != screenComposer {
		if err := sess.OpenComposer(); err != nil {
			return nil, err
		}
		s.current = screenComposer
	}
	return sess, nil
}

func (s *server) close() {
	if s.sess != nil {
		s.sess.Close()
		s.sess = nil
		s.current = screenNone
	}
}

// outPath resolves a caller-supplied path, falling back to a timestamped name
// in the output directory so tools always have somewhere to write.
func (s *server) outPath(given, prefix, ext string) (string, error) {
	if given != "" {
		if err := os.MkdirAll(filepath.Dir(given), 0o755); err != nil {
			return "", err
		}
		return given, nil
	}
	if err := os.MkdirAll(s.outDir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s%s", prefix, time.Now().Format("20060102-150405.000"), ext)
	return filepath.Join(s.outDir, name), nil
}

func writePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func encodePNG(img image.Image) ([]byte, error) {
	f, err := os.CreateTemp("", "mp-*.png")
	if err != nil {
		return nil, err
	}
	name := f.Name()
	defer os.Remove(name)
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return nil, err
	}
	f.Close()
	return os.ReadFile(name)
}

// parseOps converts the wire form of a drawing operation into mp.Op,
// resolving colour names and validating what the game can actually do.
func parseOps(in []opInput) ([]mp.Op, error) {
	out := make([]mp.Op, 0, len(in))
	for i, o := range in {
		color, err := mp.ParseColor(o.Color)
		if err != nil {
			return nil, fmt.Errorf("operation %d: %w", i, err)
		}
		if len(o.Points) == 0 {
			return nil, fmt.Errorf("operation %d (%s): no points given", i, o.Kind)
		}
		pts := make([]mp.Point, len(o.Points))
		for j, p := range o.Points {
			pts[j] = mp.Point{X: p.X, Y: p.Y}
		}
		out = append(out, mp.Op{
			Kind:    mp.OpKind(o.Kind),
			Points:  pts,
			Color:   color,
			Size:    o.Size,
			Filled:  o.Filled,
			Density: o.Density,
		})
	}
	return out, nil
}
