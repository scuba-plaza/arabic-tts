package stt

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/scuba-plaza/arabic-tts/arabic"
)

func sample() *Transcript {
	return &Transcript{
		Source:   "audio/test.wav",
		Language: "ar-XA",
		Model:    "chirp_3",
		Duration: 20,
		Segments: []SegmentResult{
			{Index: 0, Start: 0, End: 3.5, Text: "السلام عليكم", Confidence: 0.94},
			{Index: 1, Start: 3.5, End: 9.25, Text: "هذا اختبار للنظام", Confidence: 0.91},
			{Index: 2, Start: 9.25, End: 20, Text: "شكرا لكم", Confidence: 0.88, HardCut: true},
		},
	}
}

func render(t *testing.T, opts WriteOptions) string {
	t.Helper()
	var buf bytes.Buffer
	if err := sample().Write(&buf, opts); err != nil {
		t.Fatalf("Write: %v", err)
	}
	return buf.String()
}

func TestSRTTiming(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatSRT})
	for _, want := range []string{
		"1\n00:00:00,000 --> 00:00:03,500",
		"2\n00:00:03,500 --> 00:00:09,250",
		"3\n00:00:09,250 --> 00:00:20,000",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing cue:\n%s\nin:\n%s", want, out)
		}
	}
}

func TestSRTCuesAreNumberedFromOne(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatSRT})
	if !strings.HasPrefix(out, "1\n") {
		t.Errorf("cue numbering should start at 1, got %.20q", out)
	}
}

func TestSubtitleCarriesRTLMark(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatSRT, RTLMark: true})
	if !strings.Contains(out, arabic.RLM+"السلام عليكم") {
		t.Error("subtitle cue should be prefixed with the right-to-left mark")
	}
	plain := render(t, WriteOptions{Format: FormatSRT})
	if strings.Contains(plain, arabic.RLM) {
		t.Error("the right-to-left mark should be opt-in")
	}
}

func TestVTTHeaderAndTiming(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatVTT})
	if !strings.HasPrefix(out, "WEBVTT\n\n") {
		t.Error("VTT output must start with the WEBVTT header")
	}
	if !strings.Contains(out, "00:00:03.500 --> 00:00:09.250") {
		t.Errorf("VTT uses a dot before milliseconds, got:\n%s", out)
	}
}

func TestClockCrossesHours(t *testing.T) {
	if got := srtTime(3723.456); got != "01:02:03,456" {
		t.Errorf("srtTime(3723.456) = %q, want 01:02:03,456", got)
	}
	if got := vttTime(0); got != "00:00:00.000" {
		t.Errorf("vttTime(0) = %q", got)
	}
}

func TestTextParagraphsOnNaturalBreaks(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatText})
	if !strings.Contains(out, "السلام عليكم\n\nهذا اختبار للنظام") {
		t.Errorf("a silence boundary should start a new paragraph, got:\n%s", out)
	}
	if !strings.Contains(out, "هذا اختبار للنظام شكرا لكم") {
		t.Errorf("a forced cut should join without a paragraph break, got:\n%s", out)
	}
}

func TestJSONShape(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatJSON})
	var parsed Transcript
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Model != "chirp_3" || len(parsed.Segments) != 3 {
		t.Errorf("unexpected decoded transcript: %+v", parsed)
	}
	if strings.Contains(out, `\u0627`) {
		t.Error("Arabic should not be escaped in JSON output")
	}
}

func TestBOMPrefix(t *testing.T) {
	out := render(t, WriteOptions{Format: FormatSRT, BOM: true})
	if !strings.HasPrefix(out, "\uFEFF") {
		t.Error("expected a UTF-8 byte order mark")
	}
}

func TestEmptySegmentsAreSkipped(t *testing.T) {
	tr := sample()
	tr.Segments[1].Text = "   "
	var buf bytes.Buffer
	if err := tr.Write(&buf, WriteOptions{Format: FormatSRT}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "3\n") {
		t.Errorf("blank segments should not consume a cue number:\n%s", buf.String())
	}
}

func TestParseFormat(t *testing.T) {
	for _, in := range []string{"txt", "JSON", "srt", "VTT"} {
		if _, err := ParseFormat(in); err != nil {
			t.Errorf("ParseFormat(%q): %v", in, err)
		}
	}
	if _, err := ParseFormat("docx"); err == nil {
		t.Error("expected an error for an unknown format")
	}
}

func TestIsSubtitle(t *testing.T) {
	if !FormatSRT.IsSubtitle() || !FormatVTT.IsSubtitle() {
		t.Error("srt and vtt carry timing")
	}
	if FormatText.IsSubtitle() || FormatJSON.IsSubtitle() {
		t.Error("txt and json are not subtitle formats")
	}
}
