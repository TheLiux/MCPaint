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

// JoinOptions tunes how segments are stitched together.
type JoinOptions struct {
	// FadeSeconds dips each seam through black, picture and sound together.
	// The console does the same thing between its own screens, so a cut that
	// fades reads as the machine changing screens rather than as an edit.
	FadeSeconds float64

	// OpenCold fades up from black at the very start.
	OpenCold bool

	// EndCold fades down to black at the very end.
	EndCold bool
}

// Join concatenates parts into a single file with a continuous audio track.
func Join(out string, parts []Part) error {
	return JoinWith(out, parts, JoinOptions{})
}

// JoinWith concatenates parts, optionally fading each seam through black.
//
// Segments are re-encoded rather than stream-copied: they come from different
// runs and a copy would leave the timestamps discontinuous.
func JoinWith(out string, parts []Part, opt JoinOptions) error {
	if len(parts) == 0 {
		return fmt.Errorf("nothing to join")
	}

	args := []string{"-y", "-loglevel", "error"}

	type stream struct {
		v, a int
		dur  float64
	}
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

		dur := p.Seconds
		if dur <= 0 {
			dur = Duration(p.Video)
		}
		streams = append(streams, stream{v: v, a: a, dur: dur})
	}

	// Build the per-segment chains first and the concat inputs separately:
	// interleaving them would let ffmpeg read a concat label as an input to
	// the next filter.
	var (
		defs   []string
		labels strings.Builder
	)
	fade := opt.FadeSeconds
	for i, s := range streams {
		vIn, aIn := fmt.Sprintf("[%d:v]", s.v), fmt.Sprintf("[%d:a]", s.a)

		fadeIn := fade > 0 && (i > 0 || opt.OpenCold)
		fadeOut := fade > 0 && (i < len(streams)-1 || opt.EndCold)
		if s.dur <= 2*fade {
			fadeIn, fadeOut = false, false
		}
		if !fadeIn && !fadeOut {
			fmt.Fprintf(&labels, "%s%s", vIn, aIn)
			continue
		}

		var vf, af []string
		if fadeIn {
			vf = append(vf, fmt.Sprintf("fade=t=in:st=0:d=%.3f", fade))
			af = append(af, fmt.Sprintf("afade=t=in:st=0:d=%.3f", fade))
		}
		if fadeOut {
			st := s.dur - fade
			vf = append(vf, fmt.Sprintf("fade=t=out:st=%.3f:d=%.3f", st, fade))
			af = append(af, fmt.Sprintf("afade=t=out:st=%.3f:d=%.3f", st, fade))
		}

		defs = append(defs,
			fmt.Sprintf("%s%s[v%d]", vIn, strings.Join(vf, ","), i),
			fmt.Sprintf("%s%s[a%d]", aIn, strings.Join(af, ","), i))
		fmt.Fprintf(&labels, "[v%d][a%d]", i, i)
	}

	var filter strings.Builder
	for _, d := range defs {
		filter.WriteString(d)
		filter.WriteString(";")
	}
	filter.WriteString(labels.String())
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
