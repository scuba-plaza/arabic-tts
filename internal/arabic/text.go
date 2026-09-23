package arabic

import (
	"strings"
	"unicode"
)

const RLM = "\u200F"

func IsTashkeel(r rune) bool {
	return (r >= 0x064B && r <= 0x0652) || r == 0x0670 || (r >= 0x06D6 && r <= 0x06ED)
}

func IsArabicLetter(r rune) bool {
	switch {
	case r >= 0x0621 && r <= 0x063A:
		return true
	case r >= 0x0641 && r <= 0x064A:
		return true
	case r >= 0x0671 && r <= 0x06D3:
		return true
	}
	return false
}

func HasArabic(s string) bool {
	for _, r := range s {
		if IsArabicLetter(r) {
			return true
		}
	}
	return false
}

func StripTashkeel(s string) string {
	return strings.Map(func(r rune) rune {
		if IsTashkeel(r) {
			return -1
		}
		return r
	}, s)
}

var normalReplacer = strings.NewReplacer(
	"أ", "ا",
	"إ", "ا",
	"آ", "ا",
	"ٱ", "ا",
	"ى", "ي",
	"ة", "ه",
	"ـ", "",
)

func Normalize(s string) string {
	s = StripTashkeel(s)
	s = normalReplacer.Replace(s)
	var b strings.Builder
	b.Grow(len(s))
	lastSpace := true
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			if !lastSpace {
				b.WriteRune(' ')
				lastSpace = true
			}
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
		default:
			b.WriteRune(r)
			lastSpace = false
		}
	}
	return strings.TrimSpace(b.String())
}
