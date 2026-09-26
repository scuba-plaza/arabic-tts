package audio

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestWriteWAVRoundTrip(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	var buf bytes.Buffer
	if err := WriteWAV(&buf, pcm, 24000, 1); err != nil {
		t.Fatalf("WriteWAV: %v", err)
	}
	out := buf.Bytes()
	if len(out) != headerSize+len(pcm) {
		t.Fatalf("wrote %d bytes, want %d", len(out), headerSize+len(pcm))
	}
	if got := binary.LittleEndian.Uint32(out[24:28]); got != 24000 {
		t.Errorf("sample rate in header is %d, want 24000", got)
	}
	got, err := StripWAVHeader(out)
	if err != nil {
		t.Fatalf("StripWAVHeader: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Errorf("round trip changed the payload: %v vs %v", got, pcm)
	}
}

func TestStripWAVHeaderSkipsExtraChunks(t *testing.T) {
	pcm := []byte{9, 9, 8, 8}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")

	buf.WriteString("LIST")
	binary.Write(&buf, binary.LittleEndian, uint32(4))
	buf.WriteString("INFO")

	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	buf.Write(make([]byte, 16))

	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(pcm)))
	buf.Write(pcm)

	got, err := StripWAVHeader(buf.Bytes())
	if err != nil {
		t.Fatalf("StripWAVHeader: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Errorf("got %v, want %v", got, pcm)
	}
}

func TestStripWAVHeaderOddSizedChunkIsPadded(t *testing.T) {
	pcm := []byte{7, 7}
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")
	buf.WriteString("odd ")
	binary.Write(&buf, binary.LittleEndian, uint32(3))
	buf.Write([]byte{1, 2, 3, 0})
	buf.WriteString("data")
	binary.Write(&buf, binary.LittleEndian, uint32(len(pcm)))
	buf.Write(pcm)

	got, err := StripWAVHeader(buf.Bytes())
	if err != nil {
		t.Fatalf("StripWAVHeader: %v", err)
	}
	if !bytes.Equal(got, pcm) {
		t.Errorf("got %v, want %v", got, pcm)
	}
}

func TestStripWAVHeaderRejectsNonWAV(t *testing.T) {
	for name, input := range map[string][]byte{
		"empty":     {},
		"truncated": []byte("RIFF"),
		"mp3":       []byte("ID3\x04\x00\x00\x00\x00\x00\x00\x00\x00"),
	} {
		if _, err := StripWAVHeader(input); err == nil {
			t.Errorf("%s: expected an error, got none", name)
		}
	}
}

func TestStripWAVHeaderMissingDataChunk(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	binary.Write(&buf, binary.LittleEndian, uint32(0))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	binary.Write(&buf, binary.LittleEndian, uint32(16))
	buf.Write(make([]byte, 16))
	if _, err := StripWAVHeader(buf.Bytes()); err == nil {
		t.Error("expected an error when there is no data chunk")
	}
}

func TestPCMOffsetIsFrameAligned(t *testing.T) {
	for _, seconds := range []float64{0, 0.5, 1.0001, 12.345} {
		if got := PCMOffset(seconds, 16000); got%2 != 0 {
			t.Errorf("offset for %vs is %d, not frame aligned", seconds, got)
		}
	}
}

func TestPCMDuration(t *testing.T) {
	if got := PCMDuration(32000, 16000); got != 1 {
		t.Errorf("32000 bytes at 16 kHz is %v seconds, want 1", got)
	}
}

func TestSilenceLength(t *testing.T) {
	if got := len(Silence(16000, 1, 0.5)); got != 16000 {
		t.Errorf("half a second at 16 kHz mono is %d bytes, want 16000", got)
	}
}
