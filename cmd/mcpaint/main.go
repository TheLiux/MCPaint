// Command mcpaint is an MCP server that creates pictures and music with
// Mario Paint running headless, and captures the result as PNG, WAV and MP4.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/TheLiux/MCPaint/internal/session"
)

const version = "0.1.0"

func main() {
	var (
		corePath = flag.String("core", os.Getenv("MCPAINT_CORE"), "libretro SNES core (.dylib/.so/.dll)")
		romPath  = flag.String("rom", os.Getenv("MCPAINT_ROM"), "Mario Paint ROM")
		outDir   = flag.String("out", os.Getenv("MCPAINT_OUT"), "directory for generated files")
	)
	flag.Parse()

	if *corePath == "" || *romPath == "" {
		fmt.Fprintln(os.Stderr,
			"mcpaint needs a libretro core and a Mario Paint ROM.\n"+
				"Set MCPAINT_CORE and MCPAINT_ROM, or pass -core and -rom.")
		os.Exit(2)
	}
	if *outDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			log.Fatal(err)
		}
		*outDir = filepath.Join(home, "mcpaint")
	}

	srv := newServer(session.Config{CorePath: *corePath, ROMPath: *romPath}, *outDir)
	defer srv.close()

	s := mcp.NewServer(&mcp.Implementation{Name: "mcpaint", Version: version}, nil)

	mcp.AddTool(s, &mcp.Tool{
		Name: "reference",
		Description: "List what Mario Paint can actually do: the palette, the " +
			"instruments, the note range, the canvas size and the song limits. " +
			"Worth calling before composing or drawing, because the constraints " +
			"are tight and unusual.",
	}, srv.reference)

	mcp.AddTool(s, &mcp.Tool{
		Name: "draw_image",
		Description: "Redraw an existing image on the Mario Paint canvas, " +
			"quantized to the game's 16 colours, and return the result.",
	}, srv.drawImage)

	mcp.AddTool(s, &mcp.Tool{
		Name: "draw",
		Description: "Draw with Mario Paint's own tools -- pencil, line, rect, " +
			"ellipse, fill and spray can. Set videoPath to record a timelapse of " +
			"the picture appearing, with the in-game cursor following the strokes.",
	}, srv.draw)

	mcp.AddTool(s, &mcp.Tool{
		Name:        "screenshot",
		Description: "Capture the canvas, or the whole screen including the toolbars.",
	}, srv.screenshot)

	mcp.AddTool(s, &mcp.Tool{
		Name: "compose",
		Description: "Write a song into Mario Paint's music composer. A song is " +
			"up to 96 columns holding at most three notes each; pitches are " +
			"diatonic staff positions, so there are no sharps or flats.",
	}, srv.compose)

	mcp.AddTool(s, &mcp.Tool{
		Name: "import_midi",
		Description: "Load a MIDI file into the composer, fitting it to what the " +
			"game can play. The report says what had to give: notes transposed by " +
			"octaves, sharps and flats snapped to the staff, and voices dropped " +
			"where a column already held three.",
	}, srv.importMIDI)

	mcp.AddTool(s, &mcp.Tool{
		Name: "play",
		Description: "Play the current song and record it. The audio is what the " +
			"SNES sound chip actually produces. Set videoPath to also capture the " +
			"composer screen scrolling along with it.",
	}, srv.play)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := s.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}
