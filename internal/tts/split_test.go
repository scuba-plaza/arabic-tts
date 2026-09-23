package tts

import (
	"strings"
	"testing"
	"unicode/utf8"

	"arabic-tts/internal/arabic"
)

const sentence = "السلام عليكم ورحمة الله وبركاته، هذا اختبار للنظام. "

func TestSplitRespectsByteBudget(t *testing.T) {
	text := strings.Repeat(sentence, 400)
	for _, budget := range []int{64, 256, 1000, 4500} {
		chunks := Split(text, budget)
		if len(chunks) == 0 {
			t.Fatalf("budget %d: no chunks produced", budget)
		}
		for i, c := range chunks {
			if len(c) > budget {
				t.Errorf("budget %d: chunk %d is %d bytes, over budget", budget, i, len(c))
			}
			if !utf8.ValidString(c) {
				t.Errorf("budget %d: chunk %d is not valid UTF-8", budget, i)
			}
		}
	}
}

func TestSplitPreservesAllText(t *testing.T) {
	text := strings.Repeat(sentence, 50)
	joined := strings.Join(Split(text, 300), " ")
	want := strings.Join(strings.Fields(text), " ")
	got := strings.Join(strings.Fields(joined), " ")
	if got != want {
		t.Errorf("text was altered by splitting\n got: %.120s\nwant: %.120s", got, want)
	}
}

func TestSplitNeverStrandsDiacritics(t *testing.T) {
	text := strings.Repeat("مُحَمَّدٌ ", 300)
	for budget := 16; budget <= 64; budget++ {
		for _, c := range Split(text, budget) {
			first, _ := utf8.DecodeRuneInString(c)
			if arabic.IsTashkeel(first) {
				t.Fatalf("budget %d: chunk starts with an orphaned diacritic: %q", budget, c)
			}
		}
	}
}

func TestSplitPrefersSentenceBoundaries(t *testing.T) {
	text := "الجملة الأولى. الجملة الثانية. الجملة الثالثة."
	chunks := Split(text, 40)
	if len(chunks) < 2 {
		t.Fatalf("expected the text to be split, got %d chunk(s)", len(chunks))
	}
	if !strings.HasSuffix(chunks[0], ".") {
		t.Errorf("first chunk should end at a sentence boundary, got %q", chunks[0])
	}
}

func TestSplitShortTextIsOneChunk(t *testing.T) {
	chunks := Split("مرحبا بك", 4500)
	if len(chunks) != 1 || chunks[0] != "مرحبا بك" {
		t.Errorf("short text should pass through untouched, got %q", chunks)
	}
}

func TestSplitEmpty(t *testing.T) {
	if got := Split("   \n  ", 100); got != nil {
		t.Errorf("blank input should produce no chunks, got %q", got)
	}
}

func TestSplitNoBoundaryFallsBackToRuneCut(t *testing.T) {
	text := strings.Repeat("ب", 5000)
	for _, c := range Split(text, 100) {
		if !utf8.ValidString(c) {
			t.Fatal("rune fallback produced invalid UTF-8")
		}
		if len(c) > 100 {
			t.Fatalf("rune fallback exceeded budget: %d bytes", len(c))
		}
	}
}
