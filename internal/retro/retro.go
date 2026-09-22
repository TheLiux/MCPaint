// Package retro is a minimal headless binding for the libretro API.
//
// The core is loaded at runtime via dlopen, so there is no static linking and
// no license contamination: libretro.h itself is MIT.
//
// The binding is deliberately single-instance. The MCP server drives one SNES
// at a time, which spares us a handle map across the cgo boundary.
package retro

/*
#cgo CFLAGS: -I${SRCDIR}
#cgo LDFLAGS: -ldl
#include "bridge.h"
#include <stdlib.h>
*/
import "C"

import (
	"fmt"
	"image"
	"sync"
	"unsafe"
)

// Memory regions exposed by the core.
const (
	MemorySaveRAM   = C.RETRO_MEMORY_SAVE_RAM
	MemorySystemRAM = C.RETRO_MEMORY_SYSTEM_RAM // WRAM: where the canvas and song live
	MemoryVideoRAM  = C.RETRO_MEMORY_VIDEO_RAM
)

// Devices for SetControllerPortDevice.
const (
	DeviceNone   = C.RETRO_DEVICE_NONE
	DeviceJoypad = C.RETRO_DEVICE_JOYPAD
	DeviceMouse  = C.RETRO_DEVICE_MOUSE
)

// SNES mouse IDs. X and Y are *relative* deltas, not positions.
const (
	MouseX     = C.RETRO_DEVICE_ID_MOUSE_X
	MouseY     = C.RETRO_DEVICE_ID_MOUSE_Y
	MouseLeft  = C.RETRO_DEVICE_ID_MOUSE_LEFT
	MouseRight = C.RETRO_DEVICE_ID_MOUSE_RIGHT
)

// MouseState is handed to the core on its next poll.
type MouseState struct {
	DX, DY      int16 // delta since the last frame
	Left, Right bool
}

// Core is a loaded libretro core.
type Core struct {
	mu sync.Mutex

	frame   *image.RGBA // most recent rendered frame
	audio   []int16     // accumulated samples, stereo interleaved
	capture bool        // when false, audio is discarded

	mouse MouseState

	loaded bool
	game   bool
}

var (
	// Global instance: the C trampolines cannot carry a Go context.
	inst   *Core
	instMu sync.Mutex
)

// Open loads the core from the given shared library.
func Open(corePath, systemDir, saveDir string) (*Core, error) {
	instMu.Lock()
	defer instMu.Unlock()
	if inst != nil {
		return nil, fmt.Errorf("a core is already open")
	}

	cSys, cSave := C.CString(systemDir), C.CString(saveDir)
	defer C.free(unsafe.Pointer(cSys))
	defer C.free(unsafe.Pointer(cSave))
	C.br_set_dirs(cSys, cSave)

	cPath := C.CString(corePath)
	defer C.free(unsafe.Pointer(cPath))

	errbuf := (*C.char)(C.malloc(512))
	defer C.free(unsafe.Pointer(errbuf))

	if rc := C.br_load(cPath, errbuf, 512); rc != 0 {
		return nil, fmt.Errorf("loading core: %s", C.GoString(errbuf))
	}

	c := &Core{loaded: true}
	inst = c
	C.br_init()
	return c, nil
}

// APIVersion reports the libretro API version the core implements.
func (c *Core) APIVersion() uint { return uint(C.br_api_version()) }

// LoadGame loads a ROM already read into memory.
func (c *Core) LoadGame(path string, data []byte) error {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	var p unsafe.Pointer
	if len(data) > 0 {
		p = unsafe.Pointer(&data[0])
	}
	if rc := C.br_load_game(cPath, p, C.size_t(len(data))); rc != 0 {
		return fmt.Errorf("the core rejected ROM %q", path)
	}
	c.game = true
	return nil
}

// AVInfo describes the geometry and timing the core reports.
type AVInfo struct {
	BaseWidth, BaseHeight uint
	MaxWidth, MaxHeight   uint
	FPS                   float64
	SampleRate            float64
}

func (c *Core) AVInfo() AVInfo {
	var info C.struct_retro_system_av_info
	C.br_get_av_info(&info)
	return AVInfo{
		BaseWidth:  uint(info.geometry.base_width),
		BaseHeight: uint(info.geometry.base_height),
		MaxWidth:   uint(info.geometry.max_width),
		MaxHeight:  uint(info.geometry.max_height),
		FPS:        float64(info.timing.fps),
		SampleRate: float64(info.timing.sample_rate),
	}
}

// Run advances one frame. On return, Frame() reflects the new frame.
//
// Mouse movement is relative, so it is consumed here at end of frame. Clearing
// it inside the callback would break the Y axis, because the core reads X and
// Y in two separate calls.
func (c *Core) Run() {
	C.br_run()
	c.mu.Lock()
	c.mouse.DX, c.mouse.DY = 0, 0
	c.mu.Unlock()
}

// Reset restarts the machine, as the console's reset button would.
func (c *Core) Reset() { C.br_reset() }

// RunFrames advances n frames.
func (c *Core) RunFrames(n int) {
	for i := 0; i < n; i++ {
		c.Run()
	}
}

// Frame returns the most recent rendered frame.
//
// The image is owned by the Core and is overwritten on the next Run, so clone
// it if you need to keep it.
func (c *Core) Frame() *image.RGBA {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.frame
}

// SetMouse sets the mouse state used by subsequent frames.
func (c *Core) SetMouse(m MouseState) {
	c.mu.Lock()
	c.mouse = m
	c.mu.Unlock()
}

// SetControllerPortDevice attaches a device to a port.
func (c *Core) SetControllerPortDevice(port, device uint) {
	C.br_set_controller_port_device(C.unsigned(port), C.unsigned(device))
}

// Memory returns a direct, *writable* view into one of the emulator's memory
// regions. Writing here is writing to the console's RAM, which is how the
// canvas and the song are injected.
func (c *Core) Memory(id uint) []byte {
	p := C.br_memory_data(C.unsigned(id))
	n := C.br_memory_size(C.unsigned(id))
	if p == nil || n == 0 {
		return nil
	}
	return unsafe.Slice((*byte)(p), int(n))
}

// StartAudioCapture clears the buffer and begins accumulating samples.
func (c *Core) StartAudioCapture() {
	c.mu.Lock()
	c.audio = c.audio[:0]
	c.capture = true
	c.mu.Unlock()
}

// StopAudioCapture returns the accumulated samples, stereo interleaved.
func (c *Core) StopAudioCapture() []int16 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.capture = false
	out := make([]int16, len(c.audio))
	copy(out, c.audio)
	c.audio = c.audio[:0]
	return out
}

// Serialize captures the core's internal state.
func (c *Core) Serialize() ([]byte, error) {
	n := C.br_serialize_size()
	if n == 0 {
		return nil, fmt.Errorf("the core does not support save states")
	}
	buf := make([]byte, int(n))
	if rc := C.br_serialize(unsafe.Pointer(&buf[0]), n); rc != 0 {
		return nil, fmt.Errorf("retro_serialize failed")
	}
	return buf, nil
}

// Unserialize restores a state captured by Serialize.
func (c *Core) Unserialize(state []byte) error {
	if len(state) == 0 {
		return fmt.Errorf("empty save state")
	}
	if rc := C.br_unserialize(unsafe.Pointer(&state[0]), C.size_t(len(state))); rc != 0 {
		return fmt.Errorf("retro_unserialize failed")
	}
	return nil
}

// Close unloads the game and the core.
func (c *Core) Close() {
	instMu.Lock()
	defer instMu.Unlock()
	if !c.loaded {
		return
	}
	if c.game {
		C.br_unload_game()
		c.game = false
	}
	C.br_deinit()
	C.br_unload()
	c.loaded = false
	inst = nil
}
