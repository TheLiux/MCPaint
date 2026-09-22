package capture

import (
	"encoding/binary"
	"os"
)

// WriteWAV writes interleaved stereo 16-bit samples as a RIFF/WAVE file.
func WriteWAV(path string, samples []int16, sampleRate int) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	const channels, bits = 2, 16
	dataLen := len(samples) * 2
	byteRate := sampleRate * channels * bits / 8

	w := func(v any) error { return binary.Write(f, binary.LittleEndian, v) }
	if _, err := f.Write([]byte("RIFF")); err != nil {
		return err
	}
	if err := w(uint32(36 + dataLen)); err != nil {
		return err
	}
	if _, err := f.Write([]byte("WAVEfmt ")); err != nil {
		return err
	}
	for _, v := range []any{
		uint32(16), uint16(1), uint16(channels), uint32(sampleRate),
		uint32(byteRate), uint16(channels * bits / 8), uint16(bits),
	} {
		if err := w(v); err != nil {
			return err
		}
	}
	if _, err := f.Write([]byte("data")); err != nil {
		return err
	}
	if err := w(uint32(dataLen)); err != nil {
		return err
	}
	return w(samples)
}
