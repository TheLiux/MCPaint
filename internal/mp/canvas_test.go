package mp

import (
	"math/rand"
	"testing"
)

func TestCanvasTileRoundTrip(t *testing.T) {
	c := NewCanvas()
	rng := rand.New(rand.NewSource(1))
	for i := range c.Pix {
		c.Pix[i] = byte(rng.Intn(16))
	}

	buf := make([]byte, CanvasBytes)
	c.Encode(buf)

	got := NewCanvas()
	got.Decode(buf)

	for i := range c.Pix {
		if got.Pix[i] != c.Pix[i] {
			t.Fatalf("pixel %d: encoded %d, decoded %d", i, c.Pix[i], got.Pix[i])
		}
	}
}

func TestNearestIsExactOnPaletteColours(t *testing.T) {
	for i, p := range Palette {
		if got := Nearest(p.R, p.G, p.B); got != byte(i) {
			t.Errorf("palette colour %d resolved to %d", i, got)
		}
	}
}

func TestEncodeMatchesKnownTileLayout(t *testing.T) {
	// A single pixel of colour 5 (0b0101) at (0,0) must light bitplanes 0 and
	// 2, i.e. byte 0 (plane 0) and byte 16 (plane 2), top bit.
	c := NewCanvas()
	c.Set(0, 0, 5)
	buf := make([]byte, CanvasBytes)
	c.Encode(buf)

	if buf[0] != 0x80 {
		t.Errorf("plane 0 byte = %02X, want 80", buf[0])
	}
	if buf[1] != 0x00 {
		t.Errorf("plane 1 byte = %02X, want 00", buf[1])
	}
	if buf[16] != 0x80 {
		t.Errorf("plane 2 byte = %02X, want 80", buf[16])
	}
	if buf[17] != 0x00 {
		t.Errorf("plane 3 byte = %02X, want 00", buf[17])
	}
}
