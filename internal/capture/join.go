package capture

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// Part is one segment of a joined video. A segment with no audio of its own
// gets exactly Seconds of silence, so the parts stay in step.
type Part struct {
	Video   string
	Audio   string
	Seconds float64
}

// Join concatenates parts into a single file with a continuous audio track.
//
// Segments are re-encoded rather than stream-copied: they come from different
// runs and a copy would leave the timestamps discontinuous.
func Join(out string, parts []Part) error {
	if len(parts) == 0 {
		return fmt.Errorf("nothing to join")
	}

	args := []string{"-y", "-loglevel", "error"}

	type stream struct{ v, a int }
	var streams []stream
	index := 0
	for _, p := range parts {
		args = append(args, "-i", p.Video)
		v := index
		index++

		a := -1
		switch {
		case p.Audio != "":
			args = append(args, "-i", p.Audio)
			a = index
			index++
		default:
			// Silence must have an explicit duration. Padding an open-ended
			// source instead makes ffmpeg generate audio forever.
			secs := p.Seconds
			if secs <= 0 {
				secs = 1
			}
			args = append(args, "-f", "lavfi", "-t", fmt.Sprintf("%.3f", secs),
				"-i", "anullsrc=r=32040:cl=stereo")
			a = index
			index++
		}
		streams = append(streams, stream{v: v, a: a})
	}

	var filter strings.Builder
	for _, s := range streams {
		fmt.Fprintf(&filter, "[%d:v][%d:a]", s.v, s.a)
	}
	fmt.Fprintf(&filter, "concat=n=%d:v=1:a=1[v][a]", len(streams))

	args = append(args,
		"-filter_complex", filter.String(),
		"-map", "[v]", "-map", "[a]",
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "16",
		"-c:a", "aac",
		out,
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "ffmpeg", args...)
	if res, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("joining video: %w: %s", err, strings.TrimSpace(string(res)))
	}
	return nil
}

// Duration asks ffprobe how long a file is, in seconds.
func Duration(path string) float64 {
	out, err := exec.Command("ffprobe", "-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1", path).Output()
	if err != nil {
		return 0
	}
	var secs float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &secs)
	return secs
}

// Mux pairs a silent video with an audio file.
func Mux(out, video, audio string) error {
	return Join(out, []Part{{Video: video, Audio: audio}})
}
