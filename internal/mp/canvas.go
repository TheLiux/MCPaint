package mp

import (
	"image"
	"image/color"
	"image/draw"
)

// Canvas is the Mario Paint drawing surface as palette indices, one byte per
// pixel, covering the whole buffer including the parts hidden behind the UI.
type Canvas struct {
	Pix []byte // CanvasBufW * CanvasBufH, values 0..15
}

func NewCanvas() *Canvas {
	return &Canvas{Pix: make([]byte, CanvasBufW*CanvasBufH)}
}

func (c *Canvas) At(x, y int) byte {
	if x < 0 || y < 0 || x >= CanvasBufW || y >= CanvasBufH {
		return 0
	}
	return c.Pix[y*CanvasBufW+x]
}

func (c *Canvas) Set(x, y int, idx byte) {
	if x < 0 || y < 0 || x >= CanvasBufW || y >= CanvasBufH {
		return
	}
	c.Pix[y*CanvasBufW+x] = idx & 0x0F
}

// Fill sets every visible pixel to idx.
func (c *Canvas) Fill(idx byte) {
	for y := VisibleY; y < VisibleY+VisibleH; y++ {
		for x := VisibleX; x < VisibleX+VisibleW; x++ {
			c.Set(x, y, idx)
		}
	}
}

// Encode writes the canvas into a 4bpp SNES tile buffer, the layout the game
// keeps at CanvasBase: tiles in row-major order, 32 bytes each, bitplanes 0-1
// interleaved in the first 16 bytes and 2-3 in the second.
func (c *Canvas) Encode(dst []byte) {
	for i := range dst[:CanvasBytes] {
		dst[i] = 0
	}
	for y := 0; y < CanvasBufH; y++ {
		row := y % 8
		for x := 0; x < CanvasBufW; x++ {
			idx := c.Pix[y*CanvasBufW+x]
			if idx == 0 {
				continue
			}
			off := ((y/8)*CanvasTilesW + x/8) * 32
			mask := byte(1) << uint(7-x%8)
			for p := 0; p < 4; p++ {
				if idx&(1<<uint(p)) != 0 {
					dst[off+(p/2)*16+row*2+p%2] |= mask
				}
			}
		}
	}
}

// Decode reads a 4bpp tile buffer back into the canvas.
func (c *Canvas) Decode(src []byte) {
	for y := 0; y < CanvasBufH; y++ {
		row := y % 8
		for x := 0; x < CanvasBufW; x++ {
			off := ((y/8)*CanvasTilesW + x/8) * 32
			mask := byte(1) << uint(7-x%8)
			var idx byte
			for p := 0; p < 4; p++ {
				if src[off+(p/2)*16+row*2+p%2]&mask != 0 {
					idx |= 1 << uint(p)
				}
			}
			c.Pix[y*CanvasBufW+x] = idx
		}
	}
}

// ToImage renders the visible area as an RGBA image.
func (c *Canvas) ToImage() *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, VisibleW, VisibleH))
	for y := 0; y < VisibleH; y++ {
		for x := 0; x < VisibleW; x++ {
			img.SetRGBA(x, y, Palette[c.At(VisibleX+x, VisibleY+y)])
		}
	}
	return img
}

// FitMode decides how a source image is mapped onto the canvas.
type FitMode string

const (
	FitContain FitMode = "contain" // scale to fit, centred, letterboxed
	FitCover   FitMode = "cover"   // scale to fill, cropped
	FitStretch FitMode = "stretch" // ignore aspect ratio
)

// DrawImage quantizes src onto the visible canvas area.
//
// Dithering is Floyd-Steinberg; it is what makes a 15-colour palette read as
// a photograph rather than as flat blobs, but it hurts flat vector art, so it
// is caller's choice.
func (c *Canvas) DrawImage(src image.Image, fit FitMode, dither bool) {
	c.DrawImageWith(src, fit, QuantizeOptions{Dither: dither})
}

// FitDefault is what to use when the caller has no opinion: fit the whole
// picture in and band whatever is left over, rather than cutting into it.
const FitDefault = FitContain

// DrawImageVivid is DrawImage with a choice of colour matching. Vivid keeps
// hue ahead of lightness, which suits flat artwork; leave it off for photos.
func (c *Canvas) DrawImageVivid(src image.Image, fit FitMode, dither, vivid bool) {
	c.DrawImageWith(src, fit, QuantizeOptions{Dither: dither, Vivid: vivid})
}

// QuantizeOptions tunes how a source image is reduced to the palette.
type QuantizeOptions struct {
	// Dither spreads the error across neighbouring pixels. It makes a
	// photograph read as one and makes flat artwork read as noise.
	Dither bool

	// Vivid keeps hue ahead of lightness when matching.
	Vivid bool

	// Rules pin particular source colours to particular palette entries,
	// overriding the match entirely. Pinned pixels take no dither error,
	// so a logo's flat areas stay flat.
	Rules []ColorRule

	// Letterbox is the palette entry that fills whatever the picture does not
	// cover once it has been fitted. Black reads as a border; leave it unset
	// for that. The canvas background would instead look like part of the
	// picture, which is worse than an obvious band.
	Letterbox *byte
}

// letterbox returns the colour to pad with.
func (o QuantizeOptions) letterbox() color.RGBA {
	if o.Letterbox != nil {
		return Palette[*o.Letterbox&0x0F]
	}
	return Palette[13] // black
}

// DrawImageWith quantizes src onto the visible canvas area.
func (c *Canvas) DrawImageWith(src image.Image, fit FitMode, opt QuantizeOptions) {
	dither, vivid := opt.Dither, opt.Vivid
	scaled := resize(src, fit, opt.letterbox())

	// Work in float so diffused error is not truncated at every pixel.
	type rgb struct{ r, g, b float64 }
	buf := make([]rgb, VisibleW*VisibleH)
	for y := 0; y < VisibleH; y++ {
		for x := 0; x < VisibleW; x++ {
			r, g, b, _ := scaled.At(x, y).RGBA()
			buf[y*VisibleW+x] = rgb{float64(r >> 8), float64(g >> 8), float64(b >> 8)}
		}
	}

	match := Nearest
	if vivid {
		match = NearestVivid
	}

	clamp := func(v float64) uint8 {
		switch {
		case v < 0:
			return 0
		case v > 255:
			return 255
		default:
			return uint8(v)
		}
	}

	for y := 0; y < VisibleH; y++ {
		for x := 0; x < VisibleW; x++ {
			p := buf[y*VisibleW+x]
			pr, pg, pb := clamp(p.r), clamp(p.g), clamp(p.b)

			if idx, pinned := matchRule(opt.Rules, pr, pg, pb); pinned {
				c.Set(VisibleX+x, VisibleY+y, idx)
				continue
			}

			idx := match(pr, pg, pb)
			c.Set(VisibleX+x, VisibleY+y, idx)

			if !dither {
				continue
			}
			q := Palette[idx]
			er, eg, eb := p.r-float64(q.R), p.g-float64(q.G), p.b-float64(q.B)
			spread := func(dx, dy int, f float64) {
				nx, ny := x+dx, y+dy
				if nx < 0 || ny < 0 || nx >= VisibleW || ny >= VisibleH {
					return
				}
				t := &buf[ny*VisibleW+nx]
				t.r += er * f
				t.g += eg * f
				t.b += eb * f
			}
			spread(1, 0, 7.0/16)
			spread(-1, 1, 3.0/16)
			spread(0, 1, 5.0/16)
			spread(1, 1, 1.0/16)
		}
	}
}

// resize scales src to exactly VisibleW x VisibleH according to fit, using
// box sampling so downscaled photos do not alias. Whatever the picture does
// not cover is filled with pad.
func resize(src image.Image, fit FitMode, pad color.RGBA) *image.RGBA {
	out := image.NewRGBA(image.Rect(0, 0, VisibleW, VisibleH))
	draw.Draw(out, out.Bounds(), &image.Uniform{pad}, image.Point{}, draw.Src)

	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == 0 || sh == 0 {
		return out
	}

	dw, dh := VisibleW, VisibleH
	ox, oy := 0, 0
	switch fit {
	case FitStretch:
		// use the full target
	case FitCover:
		if s := max(float64(dw)/float64(sw), float64(dh)/float64(sh)); s > 0 {
			dw, dh = int(float64(sw)*s+0.5), int(float64(sh)*s+0.5)
			ox, oy = (VisibleW-dw)/2, (VisibleH-dh)/2
		}
	default: // contain
		if s := min(float64(dw)/float64(sw), float64(dh)/float64(sh)); s > 0 {
			dw, dh = int(float64(sw)*s+0.5), int(float64(sh)*s+0.5)
			ox, oy = (VisibleW-dw)/2, (VisibleH-dh)/2
		}
	}

	for dy := 0; dy < dh; dy++ {
		for dx := 0; dx < dw; dx++ {
			tx, ty := ox+dx, oy+dy
			if tx < 0 || ty < 0 || tx >= VisibleW || ty >= VisibleH {
				continue
			}
			// Average the source box that maps onto this destination pixel.
			x0 := sb.Min.X + dx*sw/dw
			x1 := sb.Min.X + (dx+1)*sw/dw
			y0 := sb.Min.Y + dy*sh/dh
			y1 := sb.Min.Y + (dy+1)*sh/dh
			if x1 <= x0 {
				x1 = x0 + 1
			}
			if y1 <= y0 {
				y1 = y0 + 1
			}
			var rs, gs, bs, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					r, g, b, _ := src.At(xx, yy).RGBA()
					rs += uint64(r >> 8)
					gs += uint64(g >> 8)
					bs += uint64(b >> 8)
					n++
				}
			}
			if n == 0 {
				continue
			}
			out.SetRGBA(tx, ty, color.RGBA{uint8(rs / n), uint8(gs / n), uint8(bs / n), 0xFF})
		}
	}
	return out
}
