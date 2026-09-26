package arabic

import "testing"

func TestStripTashkeel(t *testing.T) {
	if got := StripTashkeel("مُحَمَّدٌ"); got != "محمد" {
		t.Errorf("StripTashkeel = %q, want محمد", got)
	}
	if got := StripTashkeel("بلا تشكيل"); got != "بلا تشكيل" {
		t.Errorf("undiacritised text should pass through, got %q", got)
	}
}

func TestNormalizeFoldsVariants(t *testing.T) {
	cases := map[string]string{
		"أحمد":               "احمد",
		"إبراهيم":            "ابراهيم",
		"آمنة":               "امنه",
		"مصطفى":              "مصطفي",
		"مدرســـة":           "مدرسه",
		"السلام عليكم!":      "السلام عليكم",
		"  فراغات   كثيرة  ": "فراغات كثيره",
	}
	for in, want := range cases {
		if got := Normalize(in); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeIsStableForComparison(t *testing.T) {
	spoken := "السَّلامُ عَلَيْكُم، هَذا اخْتِبار."
	written := "السلام عليكم هذا اختبار"
	if Normalize(spoken) != Normalize(written) {
		t.Errorf("the same sentence should normalise identically:\n%q\n%q",
			Normalize(spoken), Normalize(written))
	}
}

func TestHasArabic(t *testing.T) {
	if !HasArabic("hello مرحبا") {
		t.Error("mixed text contains Arabic")
	}
	if HasArabic("hello world 123") {
		t.Error("Latin text contains no Arabic")
	}
	if HasArabic("") {
		t.Error("empty text contains no Arabic")
	}
}

func TestIsTashkeel(t *testing.T) {
	for _, r := range []rune{'ً', 'ْ', 'ٰ'} {
		if !IsTashkeel(r) {
			t.Errorf("U+%04X should be a diacritic", r)
		}
	}
	for _, r := range []rune{'ا', 'ب', 'a', ' '} {
		if IsTashkeel(r) {
			t.Errorf("%q should not be a diacritic", r)
		}
	}
}
