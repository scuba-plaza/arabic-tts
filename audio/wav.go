package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	headerSize    = 44
	bytesPerFrame = 2
)

var ErrNotWAV = errors.New("not a RIFF/WAVE stream")

func WAVHeader(sampleRate, channels, dataLen int) []byte {
	h := make([]byte, headerSize)
	byteRate := sampleRate * channels * bytesPerFrame
	copy(h[0:4], "RIFF")
	binary.LittleEndian.PutUint32(h[4:8], uint32(36+dataLen))
	copy(h[8:12], "WAVE")
	copy(h[12:16], "fmt ")
	binary.LittleEndian.PutUint32(h[16:20], 16)
	binary.LittleEndian.PutUint16(h[20:22], 1)
	binary.LittleEndian.PutUint16(h[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(h[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(h[28:32], uint32(byteRate))
	binary.LittleEndian.PutUint16(h[32:34], uint16(channels*bytesPerFrame))
	binary.LittleEndian.PutUint16(h[34:36], 16)
	copy(h[36:40], "data")
	binary.LittleEndian.PutUint32(h[40:44], uint32(dataLen))
	return h
}

func WriteWAV(w io.Writer, pcm []byte, sampleRate, channels int) error {
	if _, err := w.Write(WAVHeader(sampleRate, channels, len(pcm))); err != nil {
		return err
	}
	_, err := w.Write(pcm)
	return err
}

func StripWAVHeader(b []byte) ([]byte, error) {
	if len(b) < 12 || string(b[0:4]) != "RIFF" || string(b[8:12]) != "WAVE" {
		return nil, ErrNotWAV
	}
	pos := 12
	for pos+8 <= len(b) {
		id := string(b[pos : pos+4])
		size := int(binary.LittleEndian.Uint32(b[pos+4 : pos+8]))
		body := pos + 8
		if id == "data" {
			end := body + size
			if size == 0 || end > len(b) {
				end = len(b)
			}
			return b[body:end], nil
		}
		if size < 0 || body+size < body {
			return nil, fmt.Errorf("wav chunk %q has invalid size", id)
		}
		pos = body + size
		if size%2 == 1 {
			pos++
		}
	}
	return nil, errors.New("wav stream has no data chunk")
}

func Silence(sampleRate, channels int, seconds float64) []byte {
	n := int(float64(sampleRate) * seconds)
	return make([]byte, n*channels*bytesPerFrame)
}

func PCMOffset(seconds float64, sampleRate int) int64 {
	off := int64(seconds * float64(sampleRate) * bytesPerFrame)
	return off - off%bytesPerFrame
}

func PCMDuration(n int64, sampleRate int) float64 {
	return float64(n) / float64(sampleRate*bytesPerFrame)
}
