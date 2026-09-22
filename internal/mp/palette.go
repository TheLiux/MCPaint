package mp

import (
	"fmt"
	"image/color"
	"math"
	"strings"
)

// Palette is Mario Paint's 16-colour canvas palette, decoded from the BGR555
// values in MPAINT/Palettes/Canvas.bin. Index 0 is the canvas background.
var Palette = [16]color.RGBA{
	{0xF6, 0xF6, 0xF6, 0xFF}, // 0  background
	{0xFF, 0x00, 0x00, 0xFF}, // 1  red
	{0xFF, 0x83, 0x00, 0xFF}, // 2  orange
	{0xFF, 0xFF, 0x00, 0xFF}, // 3  yellow
	{0x00, 0xFF, 0x00, 0xFF}, // 4  green
	{0x00, 0x83, 0x41, 0xFF}, // 5  dark green
	{0x00, 0xFF, 0xFF, 0xFF}, // 6  cyan
	{0x00, 0x00, 0xFF, 0xFF}, // 7  blue
	{0xC5, 0x41, 0x20, 0xFF}, // 8  brown
	{0x83, 0x62, 0x00, 0xFF}, // 9  olive
	{0xFF, 0xC5, 0x83, 0xFF}, // 10 peach
	{0xC5, 0x00, 0xC5, 0xFF}, // 11 magenta
	{0xFF, 0xFF, 0xFF, 0xFF}, // 12 white
	{0x00, 0x00, 0x00, 0xFF}, // 13 black
	{0x83, 0x83, 0x83, 0xFF}, // 14 grey
	{0xC5, 0xC5, 0xC5, 0xFF}, // 15 light grey
}

// linearize converts an sRGB channel to linear light, so colour distance is
// measured perceptually rather than on gamma-encoded values.
func linearize(v uint8) float64 {
	f := float64(v) / 255
	if f <= 0.04045 {
		return f / 12.92
	}
	return math.Pow((f+0.055)/1.055, 2.4)
}

var paletteLinear = func() [16][3]float64 {
	var out [16][3]float64
	for i, c := range Palette {
		out[i] = [3]float64{linearize(c.R), linearize(c.G), linearize(c.B)}
	}
	return out
}()

// Nearest returns the palette index closest to the given colour.
func Nearest(r, g, b uint8) byte {
	lr, lg, lb := linearize(r), linearize(g), linearize(b)
	best, bestD := byte(0), math.Inf(1)
	for i, p := range paletteLinear {
		// Weights approximate luminance sensitivity.
		dr, dg, db := lr-p[0], lg-p[1], lb-p[2]
		d := 0.2126*dr*dr + 0.7152*dg*dg + 0.0722*db*db
		if d < bestD {
			best, bestD = byte(i), d
		}
	}
	return best
}

// colourNames label the palette in index order, for callers that would rather
// say "red" than 1.
var colourNames = [16]string{
	"background", "red", "orange", "yellow", "green", "dark-green",
	"cyan", "blue", "brown", "olive", "peach", "magenta",
	"white", "black", "grey", "light-grey",
}

// ColorName returns the name of a palette index.
func ColorName(i byte) string {
	if int(i) >= len(colourNames) {
		return ""
	}
	return colourNames[i]
}

// Colors lists the palette names in index order.
func Colors() []string { return colourNames[:] }

// ParseColor accepts a colour name, a palette index 0..15, or a #RRGGBB
// value, which is matched to the nearest palette entry.
func ParseColor(s string) (byte, error) {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return 0, fmt.Errorf("no colour given")
	}
	for i, name := range colourNames {
		if name == t {
			return byte(i), nil
		}
	}
	if strings.HasPrefix(t, "#") && len(t) == 7 {
		var r, g, b uint8
		if _, err := fmt.Sscanf(t, "#%02x%02x%02x", &r, &g, &b); err == nil {
			return Nearest(r, g, b), nil
		}
	}
	var n int
	if _, err := fmt.Sscanf(t, "%d", &n); err == nil && n >= 0 && n < len(colourNames) {
		return byte(n), nil
	}
	return 0, fmt.Errorf("unknown colour %q (want a name, 0..15, or #RRGGBB)", s)
}
