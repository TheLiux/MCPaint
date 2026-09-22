package session

import (
	"github.com/TheLiux/MCPaint/internal/capture"
	"github.com/TheLiux/MCPaint/internal/mp"
)

// DrawOptions controls how a batch of drawing operations is played out.
type DrawOptions struct {
	// Video, when set, receives one frame per drawing tick, producing the
	// timelapse of the picture appearing.
	Video *capture.Video

	// TargetSeconds is the wall-clock length to aim for when recording. The
	// number of drawing steps folded into each frame is derived from it, so a
	// sprawling picture speeds up instead of running for ten minutes.
	TargetSeconds float64

	// FPS the video is being encoded at; used with TargetSeconds.
	FPS float64

	// HoldFrames are extra frames appended after the drawing finishes, so the
	// result is on screen long enough to register.
	HoldFrames int

	// CaptureAudio collects the canvas background music over the same frames
	// as the video, so the two need no resynchronising afterwards.
	CaptureAudio bool
}

// Draw applies ops to the live canvas.
//
// Every op is applied to an in-memory canvas and written back to WRAM as it
// progresses, with the game's own cursor dragged along the path. The game
// keeps rendering normally throughout, so what gets captured is Mario Paint
// drawing the picture, not a picture pasted into Mario Paint.
func (s *Session) Draw(ops []mp.Op, opt DrawOptions) ([]int16, error) {
	canvas := s.Canvas()

	stepsPerFrame := 1
	if opt.Video != nil {
		total := countSteps(canvas, ops)
		fps := opt.FPS
		if fps <= 0 {
			fps = 30
		}
		secs := opt.TargetSeconds
		if secs <= 0 {
			secs = 8
		}
		if budget := int(fps * secs); budget > 0 && total > budget {
			stepsPerFrame = (total + budget - 1) / budget
		}
	}

	if opt.CaptureAudio {
		s.core.StartAudioCapture()
	}

	pending := 0
	var err error
	flush := func(x, y int) {
		s.SetCanvas(canvas)
		s.SetCursor(mp.VisibleX+x, mp.VisibleY+y+mp.ScreenYOffset)
		s.core.Run()
		if opt.Video != nil && err == nil {
			err = opt.Video.Write(s.core.Frame())
		}
	}

	for _, op := range ops {
		mp.Apply(canvas, op, func(x, y int) {
			pending++
			if pending < stepsPerFrame {
				return
			}
			pending = 0
			flush(x, y)
		})
		// When drawing slowly, show the tail of each operation before the
		// next begins. Under heavy pacing that would cost a frame per
		// operation, which a picture built from thousands of short strokes
		// cannot afford, so the step budget takes over instead.
		if stepsPerFrame == 1 && len(op.Points) > 0 {
			last := op.Points[len(op.Points)-1]
			flush(last.X, last.Y)
		}
	}

	s.SetCanvas(canvas)
	for i := 0; i < opt.HoldFrames; i++ {
		s.core.Run()
		if opt.Video != nil && err == nil {
			err = opt.Video.Write(s.core.Frame())
		}
	}

	var samples []int16
	if opt.CaptureAudio {
		samples = s.core.StopAudioCapture()
	}
	return samples, err
}

// countSteps replays the ops on a throwaway copy just to count the ticks, so
// the recorder can pace itself before drawing anything for real.
func countSteps(base *mp.Canvas, ops []mp.Op) int {
	tmp := mp.NewCanvas()
	copy(tmp.Pix, base.Pix)
	n := 0
	for _, op := range ops {
		mp.Apply(tmp, op, func(int, int) { n++ })
	}
	return n
}
