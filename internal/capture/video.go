// Package capture turns emulator output into files: PNG stills, WAV audio and
// MP4 video, the last two by piping raw data through ffmpeg.
package capture

import (
	"fmt"
	"image"
	"io"
	"os/exec"
)

// Video encodes RGBA frames to an MP4 by streaming them into ffmpeg.
type Video struct {
	cmd  *exec.Cmd
	pipe io.WriteCloser
	w, h int
}

// NewVideo starts an encoder for frames of the given size at fps.
//
// The SNES frame is tiny, so it is scaled up with nearest-neighbour: anything
// smoother would blur pixel art into mush.
func NewVideo(path string, w, h int, fps float64, scale int) (*Video, error) {
	if scale < 1 {
		scale = 1
	}
	args := []string{
		"-y", "-loglevel", "error",
		"-f", "rawvideo", "-pixel_format", "rgba",
		"-video_size", fmt.Sprintf("%dx%d", w, h),
		"-framerate", fmt.Sprintf("%f", fps),
		"-i", "pipe:0",
		"-vf", fmt.Sprintf("scale=%d:%d:flags=neighbor", w*scale, h*scale),
		"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "16",
		path,
	}
	cmd := exec.Command("ffmpeg", args...)
	pipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting ffmpeg: %w", err)
	}
	return &Video{cmd: cmd, pipe: pipe, w: w, h: h}, nil
}

// Write appends one frame.
func (v *Video) Write(img *image.RGBA) error {
	if img == nil {
		return nil
	}
	if img.Rect.Dx() != v.w || img.Rect.Dy() != v.h {
		return fmt.Errorf("frame is %dx%d, encoder expects %dx%d",
			img.Rect.Dx(), img.Rect.Dy(), v.w, v.h)
	}
	// Rows may be padded, so write row by row rather than the whole slice.
	row := v.w * 4
	for y := 0; y < v.h; y++ {
		off := y * img.Stride
		if _, err := v.pipe.Write(img.Pix[off : off+row]); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes the stream and waits for ffmpeg to finish writing the file.
func (v *Video) Close() error {
	if err := v.pipe.Close(); err != nil {
		return err
	}
	return v.cmd.Wait()
}
