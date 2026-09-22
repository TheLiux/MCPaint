package retro

/*
#include "bridge.h"
*/
import "C"

import (
	"image"
	"unsafe"
)

// Pixel formats (RETRO_ENVIRONMENT_SET_PIXEL_FORMAT).
const (
	pixFmt0RGB1555 = 0
	pixFmtXRGB8888 = 1
	pixFmtRGB565   = 2
)

//export goVideoRefresh
func goVideoRefresh(data unsafe.Pointer, width, height C.unsigned, pitch C.size_t) {
	c := inst
	// data == nil means "duplicate frame": the previous one stays valid.
	if c == nil || data == nil || width == 0 || height == 0 {
		return
	}

	w, h, p := int(width), int(height), int(pitch)

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.frame == nil || c.frame.Rect.Dx() != w || c.frame.Rect.Dy() != h {
		c.frame = image.NewRGBA(image.Rect(0, 0, w, h))
	}

	switch C.br_pixel_format() {
	case pixFmtXRGB8888:
		src := unsafe.Slice((*byte)(data), p*h)
		for y := 0; y < h; y++ {
			row := src[y*p:]
			dst := c.frame.Pix[y*c.frame.Stride:]
			for x := 0; x < w; x++ {
				// little-endian XRGB8888 is B,G,R,X in memory
				dst[x*4+0] = row[x*4+2]
				dst[x*4+1] = row[x*4+1]
				dst[x*4+2] = row[x*4+0]
				dst[x*4+3] = 0xFF
			}
		}
	case pixFmtRGB565:
		src := unsafe.Slice((*uint16)(data), (p/2)*h)
		stride := p / 2
		for y := 0; y < h; y++ {
			row := src[y*stride:]
			dst := c.frame.Pix[y*c.frame.Stride:]
			for x := 0; x < w; x++ {
				v := row[x]
				r := uint8((v >> 11) & 0x1F)
				g := uint8((v >> 5) & 0x3F)
				b := uint8(v & 0x1F)
				dst[x*4+0] = r<<3 | r>>2
				dst[x*4+1] = g<<2 | g>>4
				dst[x*4+2] = b<<3 | b>>2
				dst[x*4+3] = 0xFF
			}
		}
	default: // 0RGB1555
		src := unsafe.Slice((*uint16)(data), (p/2)*h)
		stride := p / 2
		for y := 0; y < h; y++ {
			row := src[y*stride:]
			dst := c.frame.Pix[y*c.frame.Stride:]
			for x := 0; x < w; x++ {
				v := row[x]
				r := uint8((v >> 10) & 0x1F)
				g := uint8((v >> 5) & 0x1F)
				b := uint8(v & 0x1F)
				dst[x*4+0] = r<<3 | r>>2
				dst[x*4+1] = g<<3 | g>>2
				dst[x*4+2] = b<<3 | b>>2
				dst[x*4+3] = 0xFF
			}
		}
	}
}

//export goAudioBatch
func goAudioBatch(data *C.int16_t, frames C.size_t) C.size_t {
	c := inst
	if c == nil {
		return frames
	}
	c.mu.Lock()
	if c.capture && data != nil && frames > 0 {
		src := unsafe.Slice((*int16)(unsafe.Pointer(data)), int(frames)*2)
		c.audio = append(c.audio, src...)
	}
	c.mu.Unlock()
	return frames
}

//export goInputPoll
func goInputPoll() {}

//export goInputState
func goInputState(port, device, index, id C.unsigned) C.int16_t {
	c := inst
	if c == nil || port != 0 || device != C.RETRO_DEVICE_MOUSE {
		return 0
	}

	c.mu.Lock()
	m := c.mouse
	c.mu.Unlock()

	switch id {
	case MouseX:
		return C.int16_t(m.DX)
	case MouseY:
		return C.int16_t(m.DY)
	case MouseLeft:
		if m.Left {
			return 1
		}
	case MouseRight:
		if m.Right {
			return 1
		}
	}
	return 0
}
