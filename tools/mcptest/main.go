// mcptest drives the MCP server end to end, the way a client would.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func show(res *mcp.CallToolResult, err error) {
	if err != nil {
		log.Fatalf("call failed: %v", err)
	}
	if res.IsError {
		for _, c := range res.Content {
			if t, ok := c.(*mcp.TextContent); ok {
				log.Fatalf("tool error: %s", t.Text)
			}
		}
		log.Fatal("tool reported an error")
	}
	for _, c := range res.Content {
		switch v := c.(type) {
		case *mcp.TextContent:
			fmt.Println("   ", v.Text)
		case *mcp.ImageContent:
			fmt.Printf("    [image %s, %d bytes]\n", v.MIMEType, len(v.Data))
		}
	}
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		fmt.Printf("    %s\n", b)
	}
}

func main() {
	ctx := context.Background()
	cmd := exec.Command("./build/mcpaint")
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	client := mcp.NewClient(&mcp.Implementation{Name: "mcptest", Version: "0"}, nil)
	sess, err := client.Connect(ctx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer sess.Close()

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("tools:")
	for _, t := range tools.Tools {
		fmt.Printf("  %-12s %.70s...\n", t.Name, t.Description)
	}

	call := func(name string, args any) {
		fmt.Printf("\n> %s\n", name)
		show(sess.CallTool(ctx, &mcp.CallToolParams{Name: name, Arguments: args}))
	}

	call("reference", map[string]any{})

	call("draw", map[string]any{
		"clear": true,
		"operations": []any{
			map[string]any{"kind": "rect", "color": "cyan", "filled": true,
				"points": []any{map[string]int{"x": 0, "y": 0}, map[string]int{"x": 247, "y": 100}}},
			map[string]any{"kind": "rect", "color": "dark-green", "filled": true,
				"points": []any{map[string]int{"x": 0, "y": 101}, map[string]int{"x": 247, "y": 163}}},
			map[string]any{"kind": "ellipse", "color": "#FFEE00", "filled": true,
				"points": []any{map[string]int{"x": 190, "y": 12}, map[string]int{"x": 230, "y": 52}}},
			map[string]any{"kind": "spray", "color": "white", "size": 5, "density": 20,
				"points": []any{map[string]int{"x": 40, "y": 30}, map[string]int{"x": 90, "y": 26}}},
		},
		"videoPath": "out/mcp_draw.mp4",
		"outPath":   "out/mcp_draw.png",
		"seconds":   4,
	})

	call("import_midi", map[string]any{
		"path":            "testdata/ode.mid",
		"stepsPerQuarter": 1,
		"tempo":           40,
		"instruments":     map[string]string{"0": "mario", "1": "gameboy"},
	})

	call("play", map[string]any{
		"seconds": 8,
		"wavPath": "out/mcp_ode.wav",
	})

	call("compose", map[string]any{
		"tempo": 40,
		"notes": []any{
			map[string]any{"column": 0, "pitch": "C4", "instrument": "mario"},
			map[string]any{"column": 2, "pitch": "E4", "instrument": "mario"},
			map[string]any{"column": 4, "pitch": "G4", "instrument": "mario"},
			map[string]any{"column": 6, "pitch": "C5", "instrument": "star"},
			map[string]any{"column": 8, "pitch": "G4", "instrument": "yoshi"},
			map[string]any{"column": 10, "pitch": "E4", "instrument": "flower"},
		},
	})

	call("play", map[string]any{
		"seconds":   6,
		"wavPath":   "out/mcp_song.wav",
		"videoPath": "out/mcp_song.mp4",
	})

	call("screenshot", map[string]any{"outPath": "out/mcp_shot.png", "fullScreen": true})

	call("record_session", map[string]any{
		"clear": true,
		"operations": []any{
			map[string]any{"kind": "rect", "color": "cyan", "filled": true,
				"points": []any{map[string]int{"x": 0, "y": 0}, map[string]int{"x": 247, "y": 110}}},
			map[string]any{"kind": "rect", "color": "green", "filled": true,
				"points": []any{map[string]int{"x": 0, "y": 111}, map[string]int{"x": 247, "y": 163}}},
			map[string]any{"kind": "ellipse", "color": "yellow", "filled": true,
				"points": []any{map[string]int{"x": 20, "y": 14}, map[string]int{"x": 56, "y": 50}}},
			map[string]any{"kind": "pencil", "color": "red", "size": 3,
				"points": []any{map[string]int{"x": 90, "y": 120}, map[string]int{"x": 130, "y": 70},
					map[string]int{"x": 170, "y": 120}, map[string]int{"x": 90, "y": 120}}},
			map[string]any{"kind": "fill", "color": "magenta",
				"points": []any{map[string]int{"x": 130, "y": 100}}},
		},
		"drawSeconds": 5,
		"songSeconds": 6,
		"outPath":     "out/mcp_session.mp4",
	})
}
