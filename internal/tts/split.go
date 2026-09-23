package tts

import (
	"strings"
	"unicode/utf8"

	"arabic-tts/internal/arabic"
)

func Split(text string, budget int) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	if budget <= 0 {
		budget = 1
	}
	var chunks []string
	for len(text) > budget {
		cut := boundary(text, budget)
		chunk := strings.TrimSpace(text[:cut])
		if chunk != "" {
			chunks = append(chunks, chunk)
		}
		text = strings.TrimSpace(text[cut:])
	}
	if text != "" {
		chunks = append(chunks, text)
	}
	return chunks
}

func boundary(s string, budget int) int {
	window := s[:budget]
	if i := lastIndexAny(window, []string{"\n\n"}); i > 0 {
		return i + len("\n\n")
	}
	if i := afterRune(window, isSentenceEnd); i > 0 {
		return i
	}
	if i := afterRune(window, isClauseEnd); i > 0 {
		return i
	}
	if i := lastIndexAny(window, []string{"\n", " ", "\t", " "}); i > 0 {
		return i + 1
	}
	return safeRuneCut(s, budget)
}

func lastIndexAny(s string, seps []string) int {
	best := -1
	for _, sep := range seps {
		if i := strings.LastIndex(s, sep); i > best {
			best = i
		}
	}
	return best
}

func isSentenceEnd(r rune) bool {
	switch r {
	case '.', '!', '?', '؟', '۔':
		return true
	}
	return false
}

func isClauseEnd(r rune) bool {
	switch r {
	case ',', ';', ':', '،', '؛':
		return true
	}
	return false
}

func afterRune(s string, match func(rune) bool) int {
	for i := len(s); i > 0; {
		r, size := utf8.DecodeLastRuneInString(s[:i])
		if r == utf8.RuneError && size <= 1 {
			i--
			continue
		}
		i -= size
		if match(r) {
			end := i + size
			for end < len(s) {
				next, nsize := utf8.DecodeRuneInString(s[end:])
				if next == '"' || next == '\'' || next == ')' || next == '»' || next == '”' {
					end += nsize
					continue
				}
				break
			}
			return end
		}
	}
	return -1
}

func safeRuneCut(s string, budget int) int {
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	for cut > 0 {
		r, _ := utf8.DecodeRuneInString(s[cut:])
		if !arabic.IsTashkeel(r) {
			break
		}
		_, size := utf8.DecodeLastRuneInString(s[:cut])
		if size == 0 {
			break
		}
		cut -= size
	}
	if cut <= 0 {
		return budget
	}
	return cut
}
