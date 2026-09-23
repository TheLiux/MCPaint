package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TestServerEndToEnd drives the server the way a client would, over stdio.
//
// It needs a ROM and a libretro core, so it skips unless both are configured.
// Everything it asserts is behaviour a client depends on: that the tools are
// advertised, that drawing and composing report what they did, and that
// playback is not silent.
func TestServerEndToEnd(t *testing.T) {
	core, rom := os.Getenv("MCPAINT_CORE"), os.Getenv("MCPAINT_ROM")
	if core == "" || rom == "" {
		t.Skip("set MCPAINT_CORE and MCPAINT_ROM to run the end-to-end test")
	}

	dir := t.TempDir()
	bin := filepath.Join(dir, "mcpaint")
	build := exec.Command("go", "build", "-o", bin, ".")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building the server: %v\n%s", err, out)
	}

	ctx := context.Background()
	cmd := exec.Command(bin, "-out", dir)
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"reference": false, "draw_image": false, "draw": false,
		"screenshot": false, "compose": false, "import_midi": false,
		"play": false, "record_session": false,
	}
	for _, tool := range tools.Tools {
		if _, ok := want[tool.Name]; ok {
			want[tool.Name] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("tool %q was not advertised", name)
		}
	}

	call := func(name string, args any, into any) {
		t.Helper()
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if res.IsError {
			t.Fatalf("%s reported an error: %+v", name, res.Content)
		}
		if into == nil {
			return
		}
		raw, err := json.Marshal(res.StructuredContent)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, into); err != nil {
			t.Fatalf("%s: decoding result: %v", name, err)
		}
	}

	call("reference", map[string]any{}, nil)

	var drew struct {
		Path   string `json:"path"`
		Width  int    `json:"width"`
		Height int    `json:"height"`
	}
	call("draw", map[string]any{
		"clear": true,
		"operations": []any{
			map[string]any{
				"kind": "rect", "color": "cyan", "filled": true,
				"points": []any{
					map[string]int{"x": 0, "y": 0},
					map[string]int{"x": 247, "y": 80},
				},
			},
		},
	}, &drew)
	if drew.Width != 248 || drew.Height != 168 {
		t.Errorf("canvas reported as %dx%d, want 248x168", drew.Width, drew.Height)
	}
	if _, err := os.Stat(drew.Path); err != nil {
		t.Errorf("draw reported %q but it is not there: %v", drew.Path, err)
	}

	var composed struct {
		NotesPlaced  int `json:"notesPlaced"`
		NotesDropped int `json:"notesDropped"`
	}
	call("compose", map[string]any{
		"tempo": 40,
		"notes": []any{
			map[string]any{"column": 0, "pitch": "C4", "instrument": "mario"},
			map[string]any{"column": 2, "pitch": "E4", "instrument": "mario"},
			map[string]any{"column": 4, "pitch": "G4", "instrument": "star"},
		},
	}, &composed)
	if composed.NotesPlaced != 3 || composed.NotesDropped != 0 {
		t.Errorf("composed %d notes and dropped %d, want 3 and 0",
			composed.NotesPlaced, composed.NotesDropped)
	}

	var played struct {
		WavPath string  `json:"wavPath"`
		Seconds float64 `json:"seconds"`
		Silent  bool    `json:"silent"`
	}
	call("play", map[string]any{"seconds": 4}, &played)
	if played.Silent {
		t.Error("playback was silent with three notes on the staff")
	}
	if played.Seconds < 3 {
		t.Errorf("recorded %.2fs, want about 4", played.Seconds)
	}
	if _, err := os.Stat(played.WavPath); err != nil {
		t.Errorf("play reported %q but it is not there: %v", played.WavPath, err)
	}

	// Playing twice in a row used to come out silent, because the playhead
	// stays parked at the end of the previous run.
	call("play", map[string]any{"seconds": 4}, &played)
	if played.Silent {
		t.Error("the second playback was silent; the playhead was not rewound")
	}
}
