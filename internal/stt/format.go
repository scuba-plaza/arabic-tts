package stt

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"arabic-tts/internal/arabic"
)

type Format string

const (
	FormatText Format = "txt"
	FormatJSON Format = "json"
	FormatSRT  Format = "srt"
	FormatVTT  Format = "vtt"
)

func ParseFormat(s string) (Format, error) {
	switch Format(strings.ToLower(s)) {
	case FormatText:
		return FormatText, nil
	case FormatJSON:
		return FormatJSON, nil
	case FormatSRT:
		return FormatSRT, nil
	case FormatVTT:
		return FormatVTT, nil
	}
	return "", fmt.Errorf("unknown format %q; use txt, json, srt or vtt", s)
}

func (f Format) IsSubtitle() bool { return f == FormatSRT || f == FormatVTT }

type WriteOptions struct {
	Format    Format
	BOM       bool
	RTLMark   bool
	Normalize bool
}

func (t *Transcript) Write(w io.Writer, opts WriteOptions) error {
	if opts.BOM {
		if _, err := io.WriteString(w, "\uFEFF"); err != nil {
			return err
		}
	}
	switch opts.Format {
	case FormatJSON:
		return t.writeJSON(w, opts)
	case FormatSRT:
		return t.writeSRT(w, opts)
	case FormatVTT:
		return t.writeVTT(w, opts)
	default:
		return t.writeText(w, opts)
	}
}

func (t *Transcript) line(s SegmentResult, opts WriteOptions) string {
	text := strings.TrimSpace(s.Text)
	if opts.Normalize {
		text = arabic.Normalize(text)
	}
	if opts.RTLMark {
		text = arabic.RLM + text
	}
	return text
}

func (t *Transcript) writeText(w io.Writer, opts WriteOptions) error {
	var b strings.Builder
	for i, s := range t.Segments {
		text := t.line(s, opts)
		if text == "" {
			continue
		}
		if i > 0 {
			if s.HardCut {
				b.WriteString(" ")
			} else {
				b.WriteString("\n\n")
			}
		}
		b.WriteString(text)
	}
	b.WriteString("\n")
	_, err := io.WriteString(w, b.String())
	return err
}

func (t *Transcript) writeJSON(w io.Writer, opts WriteOptions) error {
	out := *t
	if opts.Normalize {
		out.Segments = make([]SegmentResult, len(t.Segments))
		copy(out.Segments, t.Segments)
		for i := range out.Segments {
			out.Segments[i].Text = arabic.Normalize(out.Segments[i].Text)
		}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(out)
}

func (t *Transcript) writeSRT(w io.Writer, opts WriteOptions) error {
	n := 0
	for _, s := range t.Segments {
		text := t.line(s, opts)
		if text == "" {
			continue
		}
		n++
		if _, err := fmt.Fprintf(w, "%d\n%s --> %s\n%s\n\n",
			n, srtTime(s.Start), srtTime(s.End), text); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transcript) writeVTT(w io.Writer, opts WriteOptions) error {
	if _, err := io.WriteString(w, "WEBVTT\n\n"); err != nil {
		return err
	}
	for _, s := range t.Segments {
		text := t.line(s, opts)
		if text == "" {
			continue
		}
		if _, err := fmt.Fprintf(w, "%s --> %s\n%s\n\n",
			vttTime(s.Start), vttTime(s.End), text); err != nil {
			return err
		}
	}
	return nil
}

func (t *Transcript) Text() string {
	parts := make([]string, 0, len(t.Segments))
	for _, s := range t.Segments {
		if text := strings.TrimSpace(s.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, " ")
}

func clockParts(seconds float64) (h, m, s, ms int) {
	if seconds < 0 {
		seconds = 0
	}
	total := int(seconds*1000 + 0.5)
	ms = total % 1000
	total /= 1000
	s = total % 60
	total /= 60
	m = total % 60
	h = total / 60
	return
}

func srtTime(seconds float64) string {
	h, m, s, ms := clockParts(seconds)
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}

func vttTime(seconds float64) string {
	h, m, s, ms := clockParts(seconds)
	return fmt.Sprintf("%02d:%02d:%02d.%03d", h, m, s, ms)
}

func formatClock(seconds float64) string {
	h, m, s, _ := clockParts(seconds)
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
