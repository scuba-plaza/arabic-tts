package audio

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Info struct {
	Duration   float64
	SampleRate int
	Channels   int
	Codec      string
}

type Interval struct {
	Start float64
	End   float64
}

type ToolError struct {
	Tool   string
	Args   []string
	Stderr string
	Err    error
}

func (e *ToolError) Error() string {
	msg := strings.TrimSpace(e.Stderr)
	if msg == "" {
		return fmt.Sprintf("%s failed: %v", e.Tool, e.Err)
	}
	return fmt.Sprintf("%s failed: %v\n%s", e.Tool, e.Err, msg)
}

func (e *ToolError) Unwrap() error { return e.Err }

const stderrTailLines = 20

func run(ctx context.Context, tool string, stdout *os.File, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, tool, args...)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	var outBuf bytes.Buffer
	if stdout != nil {
		cmd.Stdout = stdout
	} else {
		cmd.Stdout = &outBuf
	}
	err := cmd.Run()
	if err != nil {
		return outBuf.String(), &ToolError{Tool: tool, Args: args, Stderr: tail(errBuf.String()), Err: err}
	}
	return outBuf.String(), nil
}

func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > stderrTailLines {
		lines = lines[len(lines)-stderrTailLines:]
	}
	return strings.Join(lines, "\n")
}

func Available() error {
	for _, tool := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(tool); err != nil {
			return fmt.Errorf("%s not found on PATH", tool)
		}
	}
	return nil
}

func Version(ctx context.Context, tool string) (string, error) {
	out, err := run(ctx, tool, nil, "-version")
	if err != nil {
		return "", err
	}
	if line, _, ok := strings.Cut(out, "\n"); ok {
		return strings.TrimSpace(line), nil
	}
	return strings.TrimSpace(out), nil
}

type probeOutput struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		CodecType  string `json:"codec_type"`
		CodecName  string `json:"codec_name"`
		SampleRate string `json:"sample_rate"`
		Channels   int    `json:"channels"`
		Duration   string `json:"duration"`
	} `json:"streams"`
}

func Probe(ctx context.Context, path string) (Info, error) {
	out, err := run(ctx, "ffprobe", nil,
		"-v", "error", "-show_format", "-show_streams", "-of", "json", path)
	if err != nil {
		return Info{}, err
	}
	var parsed probeOutput
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		return Info{}, fmt.Errorf("parsing ffprobe output for %s: %w", path, err)
	}
	info := Info{Duration: parseFloat(parsed.Format.Duration)}
	for _, s := range parsed.Streams {
		if s.CodecType != "audio" {
			continue
		}
		info.Codec = s.CodecName
		info.Channels = s.Channels
		info.SampleRate = int(parseFloat(s.SampleRate))
		if info.Duration == 0 {
			info.Duration = parseFloat(s.Duration)
		}
		break
	}
	if info.Codec == "" {
		return Info{}, fmt.Errorf("%s contains no audio stream", path)
	}
	return info, nil
}

func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}

func DecodePCM(ctx context.Context, path string, sampleRate int) (*os.File, int64, error) {
	tmp, err := os.CreateTemp("", "arabic-tts-*.pcm")
	if err != nil {
		return nil, 0, fmt.Errorf("creating decode buffer: %w", err)
	}
	_, err = run(ctx, "ffmpeg", tmp,
		"-hide_banner", "-nostdin", "-v", "error",
		"-i", path,
		"-vn", "-ac", "1", "-ar", strconv.Itoa(sampleRate),
		"-f", "s16le", "-",
	)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, err
	}
	size, err := tmp.Seek(0, 2)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("sizing decoded audio: %w", err)
	}
	if size == 0 {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, fmt.Errorf("%s decoded to zero audio samples", path)
	}
	return tmp, size, nil
}

var (
	silenceStartRe = regexp.MustCompile(`silence_start:\s*(-?[0-9.]+)`)
	silenceEndRe   = regexp.MustCompile(`silence_end:\s*(-?[0-9.]+)`)
)

func DetectSilence(ctx context.Context, path string, noiseDB float64, minDur float64) ([]Interval, error) {
	cmd := exec.CommandContext(ctx, "ffmpeg",
		"-hide_banner", "-nostdin",
		"-i", path,
		"-af", fmt.Sprintf("silencedetect=noise=%gdB:d=%g", noiseDB, minDur),
		"-f", "null", "-",
	)
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	cmd.Stdout = nil
	if err := cmd.Run(); err != nil {
		return nil, &ToolError{Tool: "ffmpeg", Stderr: tail(errBuf.String()), Err: err}
	}
	var intervals []Interval
	var open = -1.0
	for _, line := range strings.Split(errBuf.String(), "\n") {
		if m := silenceStartRe.FindStringSubmatch(line); m != nil {
			open = parseFloat(m[1])
			if open < 0 {
				open = 0
			}
			continue
		}
		if m := silenceEndRe.FindStringSubmatch(line); m != nil && open >= 0 {
			end := parseFloat(m[1])
			if end > open {
				intervals = append(intervals, Interval{Start: open, End: end})
			}
			open = -1
		}
	}
	return intervals, nil
}

func Encode(ctx context.Context, wavPath, outPath string) error {
	_, err := run(ctx, "ffmpeg", nil,
		"-hide_banner", "-nostdin", "-v", "error", "-y",
		"-i", wavPath, outPath,
	)
	return err
}
