package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/session"
)

type recordSessionInput struct {
	Operations  []opInput `json:"operations,omitempty" jsonschema:"drawing to perform; omit to record only the song already loaded"`
	Clear       bool      `json:"clear,omitempty" jsonschema:"wipe the canvas before drawing"`
	DrawSeconds float64   `json:"drawSeconds,omitempty" jsonschema:"target length of the drawing segment, default 8"`
	SongSeconds float64   `json:"songSeconds,omitempty" jsonschema:"length of the playback segment, default 10"`
	OutPath     string    `json:"outPath,omitempty" jsonschema:"where to write the MP4"`
	Music       string    `json:"music,omitempty" jsonschema:"canvas track for the drawing half: theme-1 (default), theme-2, your-song or off"`
}

type recordSessionOutput struct {
	VideoPath string  `json:"videoPath"`
	Seconds   float64 `json:"seconds"`
	Silent    bool    `json:"silent"`
}

// recordSession stitches the drawing and the playback into one clip. The two
// halves come from different screens and therefore different runs, so they are
// captured separately and joined afterwards.
func (s *server) recordSession(_ context.Context, _ *mcp.CallToolRequest, in recordSessionInput) (*mcp.CallToolResult, recordSessionOutput, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.setTrack(in.Music); err != nil {
		return nil, recordSessionOutput{}, err
	}

	outPath, err := s.outPath(in.OutPath, "session", ".mp4")
	if err != nil {
		return nil, recordSessionOutput{}, err
	}
	tmp, err := os.MkdirTemp("", "mp-session-")
	if err != nil {
		return nil, recordSessionOutput{}, err
	}
	defer os.RemoveAll(tmp)

	var parts []capture.Part

	if len(in.Operations) > 0 {
		sess, err := s.freshCanvas()
		if err != nil {
			return nil, recordSessionOutput{}, err
		}
		ops, err := parseOps(in.Operations)
		if err != nil {
			return nil, recordSessionOutput{}, err
		}
		if in.Clear {
			sess.SetCanvas(newBlankCanvas())
			sess.RunFrames(4)
		}

		drawPath := filepath.Join(tmp, "draw.mp4")
		f := sess.Frame()
		v, err := capture.NewVideo(drawPath, f.Rect.Dx(), f.Rect.Dy(), sess.FPS(), 3)
		if err != nil {
			return nil, recordSessionOutput{}, err
		}
		samples, err := sess.Draw(ops, session.DrawOptions{
			TargetSeconds: in.DrawSeconds,
			FPS:           sess.FPS(),
			Video:         v,
			HoldFrames:    int(sess.FPS() * 1.5),
			CaptureAudio:  true,
		})
		if cerr := v.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			return nil, recordSessionOutput{}, err
		}

		// The canvas music plays while the picture is drawn, so the first
		// segment carries its own soundtrack rather than silence.
		drawAudio := filepath.Join(tmp, "draw.wav")
		if err := capture.WriteWAV(drawAudio, samples, sess.SampleRate()); err != nil {
			return nil, recordSessionOutput{}, err
		}
		parts = append(parts, capture.Part{Video: drawPath, Audio: drawAudio})
	}

	sess, err := s.composer()
	if err != nil {
		return nil, recordSessionOutput{}, err
	}
	secs := in.SongSeconds
	if secs <= 0 {
		secs = 10
	}
	songVideo := filepath.Join(tmp, "song.mp4")
	songAudio := filepath.Join(tmp, "song.wav")

	f := sess.Frame()
	v, err := capture.NewVideo(songVideo, f.Rect.Dx(), f.Rect.Dy(), sess.FPS(), 3)
	if err != nil {
		return nil, recordSessionOutput{}, err
	}
	samples, err := sess.Play(session.PlayOptions{Frames: int(secs * sess.FPS()), Video: v})
	if cerr := v.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return nil, recordSessionOutput{}, err
	}
	if err := capture.WriteWAV(songAudio, samples, sess.SampleRate()); err != nil {
		return nil, recordSessionOutput{}, err
	}
	parts = append(parts, capture.Part{Video: songVideo, Audio: songAudio})

	if err := capture.JoinWith(outPath, parts, capture.JoinOptions{
		FadeSeconds: 0.6,
		OpenCold:    true,
		EndCold:     true,
	}); err != nil {
		return nil, recordSessionOutput{}, err
	}

	var peak int16
	for _, x := range samples {
		if x > peak {
			peak = x
		}
	}
	total := capture.Duration(outPath)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{
			Text: fmt.Sprintf("Recorded %d segments (%.1fs) to %s", len(parts), total, outPath),
		}},
	}, recordSessionOutput{VideoPath: outPath, Seconds: total, Silent: peak < 64}, nil
}
