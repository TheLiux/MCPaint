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

// oklab converts linear sRGB to Oklab.
//
// Plain luminance-weighted distance collapses saturated colours onto grey:
// Google blue landed nearer the palette's grey than its blue, because the
// weighting leans so heavily on the green channel. Oklab keeps hue and chroma
// in the comparison, which matters when the palette has only sixteen entries
// and enormous gaps between them.
func oklab(r, g, b float64) [3]float64 {
	l := math.Cbrt(0.4122214708*r + 0.5363325363*g + 0.0514459929*b)
	m := math.Cbrt(0.2119034982*r + 0.6806995451*g + 0.1073969566*b)
	s := math.Cbrt(0.0883024619*r + 0.2817188376*g + 0.6299787005*b)
	return [3]float64{
		0.2104542553*l + 0.7936177850*m - 0.0040720468*s,
		1.9779984951*l - 2.4285922050*m + 0.4505937099*s,
		0.0259040371*l + 0.7827717662*m - 0.8086757660*s,
	}
}

var paletteLab = func() [16][3]float64 {
	var out [16][3]float64
	for i, c := range Palette {
		out[i] = oklab(linearize(c.R), linearize(c.G), linearize(c.B))
	}
	return out
}()

// Nearest returns the palette index closest to the given colour.
func Nearest(r, g, b uint8) byte { return nearest(r, g, b, 1) }

// NearestVivid matches on hue and chroma ahead of lightness.
//
// The palette's primaries are brutal -- its blue is #0000FF, dark and fully
// saturated -- so a light, gentle brand blue is genuinely closer to grey than
// to it. That is the right answer for a photograph and the wrong one for a
// logo, where keeping the hue matters more than keeping the brightness.
func NearestVivid(r, g, b uint8) byte { return nearest(r, g, b, 0.25) }

func nearest(r, g, b uint8, lightWeight float64) byte {
	want := oklab(linearize(r), linearize(g), linearize(b))
	best, bestD := byte(0), math.Inf(1)
	for i, p := range paletteLab {
		dl, da, db := want[0]-p[0], want[1]-p[1], want[2]-p[2]
		d := lightWeight*dl*dl + da*da + db*db
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

// ColorRule pins a source colour to a palette entry.
//
// Brand colours often have no honest nearest neighbour here: Google's amber
// yellow sits between the palette's lemon and its olive, so perceptual
// matching lands on peach and the logo comes out wrong whatever the metric.
// Naming the substitution is more truthful than tuning weights until one
// example happens to pass.
type ColorRule struct {
	R, G, B   uint8
	Tolerance float64 // 0..1 in Oklab distance; 0.1 is a reasonable default
	To        byte
}

// ParseColorRule reads "#4285F4=blue" or "#4285F4=blue@0.15".
func ParseColorRule(s string) (ColorRule, error) {
	var rule ColorRule
	spec, tol := s, 0.12
	if i := strings.LastIndex(s, "@"); i >= 0 {
		spec = s[:i]
		if _, err := fmt.Sscanf(s[i+1:], "%f", &tol); err != nil {
			return rule, fmt.Errorf("bad tolerance in %q", s)
		}
	}
	from, to, ok := strings.Cut(spec, "=")
	if !ok {
		return rule, fmt.Errorf("bad rule %q (want #RRGGBB=colour)", s)
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(strings.TrimSpace(from), "#%02x%02x%02x", &r, &g, &b); err != nil {
		return rule, fmt.Errorf("bad source colour in %q", s)
	}
	idx, err := ParseColor(to)
	if err != nil {
		return rule, err
	}
	return ColorRule{R: r, G: g, B: b, Tolerance: tol, To: idx}, nil
}

// ParseColorRules reads a comma-separated list of rules.
func ParseColorRules(s string) ([]ColorRule, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, nil
	}
	var out []ColorRule
	for _, part := range strings.Split(s, ",") {
		r, err := ParseColorRule(part)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// match returns the pinned palette entry for a colour, if a rule covers it.
func matchRule(rules []ColorRule, r, g, b uint8) (byte, bool) {
	if len(rules) == 0 {
		return 0, false
	}
	want := oklab(linearize(r), linearize(g), linearize(b))
	for _, rule := range rules {
		have := oklab(linearize(rule.R), linearize(rule.G), linearize(rule.B))
		dl, da, db := want[0]-have[0], want[1]-have[1], want[2]-have[2]
		if math.Sqrt(dl*dl+da*da+db*db) <= rule.Tolerance {
			return rule.To, true
		}
	}
	return 0, false
}
