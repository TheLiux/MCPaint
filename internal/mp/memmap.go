// Package mp holds the Mario Paint domain: memory layout, palette, and the
// codecs that turn images and songs into the structures the game itself uses.
//
// Addresses are offsets into WRAM (bank $7E) and come from the labelled RAM
// map in Yoshifanatic1/Mario-Paint-Disassembly, cross-checked against the
// running game.
package mp

const (
	// Canvas graphics buffer: 32x22 SNES 4bpp tiles, 32 bytes each.
	CanvasBase   = 0xA000
	CanvasTilesW = 32
	CanvasTilesH = 22
	CanvasBufW   = CanvasTilesW * 8 // 256
	CanvasBufH   = CanvasTilesH * 8 // 176
	CanvasBytes  = CanvasTilesW * CanvasTilesH * 32

	AnimCellBase = 0x4000

	// Song: 96 columns of 3 channels, each (note, instrument).
	SongBase     = 0x09E4
	SongColumns  = 96
	SongChannels = 3
	SongBytes    = SongColumns * SongChannels * 2

	SongScrollEnd     = 0x0C24 // little-endian
	SongLoop          = 0x0C26 // 0 = off
	SongTempo         = 0x0C2D // $01 slowest .. $FF fastest, $00 pause
	SongTimeSignature = 0x0C32 // 0 = 3/4, 1 = 4/4

	CursorX   = 0x04DC
	CursorY   = 0x04DE
	MouseDX   = 0x04C6
	MouseDY   = 0x04C8
	HeldP1    = 0x0132
	PressedP1 = 0x013A
	DemoFlag  = 0x04E2

	CurrentTool      = 0x04D0
	SprayCanSelected = 0x00B8
	EraseSelected    = 0x1992
	EraseSize        = 0x1994
	PaletteRow       = 0x00A6
)

// Visible canvas area, measured against the running game: buffer rows above
// VisibleY sit behind the palette bar and rows below sit behind the toolbar.
const (
	VisibleX = 4
	VisibleY = 12
	VisibleW = 248
	VisibleH = 164

	// A buffer pixel at (x, y) appears on screen at (x, y+ScreenYOffset).
	ScreenYOffset = 15
)
