// Explore: click along the bottom toolbar and see which screens open.
package main

import (
	"crypto/sha1"
	"fmt"
	"image/png"
	"log"
	"os"

	"github.com/TheLiux/MCPaint/internal/retro"
	"github.com/TheLiux/MCPaint/internal/session"
)

func hash(s *session.Session) string {
	f := s.Frame()
	h := sha1.Sum(f.Pix)
	return fmt.Sprintf("%x", h[:5])
}

func main() {
	s, err := session.Open(session.Config{
		CorePath: os.Getenv("MCPAINT_CORE"),
		ROMPath:  os.Getenv("MCPAINT_ROM"),
	})
	if err != nil {
		log.Fatal(err)
	}
	defer s.Close()
	if err := s.BootToCanvas(); err != nil {
		log.Fatal(err)
	}
	base, err := s.Core().Serialize()
	if err != nil {
		log.Fatal(err)
	}

	os.MkdirAll("out/toolbar", 0o755)
	seen := map[string]string{}

	for y := 204; y <= 216; y += 6 {
		for x := 4; x < 252; x += 6 {
			s.Core().Unserialize(base)
			for i := 0; i < 3; i++ {
				s.SetCursor(x, y)
				s.Core().SetMouse(retro.MouseState{})
				s.Core().Run()
			}
			for i := 0; i < 6; i++ {
				s.SetCursor(x, y)
				s.Core().SetMouse(retro.MouseState{Left: true})
				s.Core().Run()
			}
			for i := 0; i < 60; i++ {
				s.SetCursor(x, y)
				s.Core().SetMouse(retro.MouseState{})
				s.Core().Run()
			}
			// Park the cursor identically in every trial, otherwise its
			// sprite alone makes each frame hash unique.
			for i := 0; i < 8; i++ {
				s.SetCursor(128, 100)
				s.Core().Run()
			}
			h := hash(s)
			if _, dup := seen[h]; dup {
				continue
			}
			seen[h] = fmt.Sprintf("%d,%d", x, y)
			name := fmt.Sprintf("out/toolbar/tb_%03d_%03d.png", x, y)
			fh, _ := os.Create(name)
			png.Encode(fh, s.Frame())
			fh.Close()
			fmt.Printf("(%3d,%3d) -> %s  %s\n", x, y, h, name)
		}
	}
	fmt.Printf("\n%d distinct screens\n", len(seen))
}
