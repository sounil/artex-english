package server

import (
	"testing"
	"unicode"

	"github.com/Autumn-27/artex/config"
)

// TestTrMsgSwitchesLanguage pins the backend output-language switching: a
// user-facing English source message is returned verbatim under en (and when
// unwired), and translated to a non-English string under ko/zh/es. This is the
// backend analogue of the web i18n catalog — it proves writeErr responses follow
// the configured language, not English only.
func TestTrMsgSwitchesLanguage(t *testing.T) {
	t.Cleanup(func() { config.CurrentLanguage = nil })

	hasCJK := func(s string) bool {
		for _, r := range s {
			if unicode.In(r, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana) {
				return true
			}
		}
		return false
	}
	hasHangul := func(s string) bool {
		for _, r := range s {
			if unicode.Is(unicode.Hangul, r) {
				return true
			}
		}
		return false
	}

	// A representative catalog key (an existing server error constant value).
	const srcMsg = errAssetIDRequired
	if srcMsg == "" || hasCJK(srcMsg) {
		t.Fatalf("precondition: source message should be English, got %q", srcMsg)
	}

	// Unwired → en passthrough.
	config.CurrentLanguage = nil
	if got := trMsg(srcMsg); got != srcMsg {
		t.Fatalf("unwired: trMsg must pass through the English source, got %q", got)
	}

	// en → passthrough (the key itself).
	config.CurrentLanguage = func() string { return "en" }
	if got := trMsg(srcMsg); got != srcMsg {
		t.Fatalf("en: trMsg must return the English source, got %q", got)
	}

	// ko/zh/es → a translated, non-empty string. ko must carry Hangul; es must not.
	for _, lang := range []string{"ko", "zh", "es"} {
		config.CurrentLanguage = func() string { return lang }
		got := trMsg(srcMsg)
		if got == "" || got == srcMsg {
			t.Fatalf("%s: trMsg must translate away from the English source, got %q", lang, got)
		}
		if lang == "ko" && !hasHangul(got) {
			t.Fatalf("ko: translation must contain Hangul, got %q", got)
		}
		if lang == "es" && hasCJK(got) {
			t.Fatalf("es: translation must not contain CJK characters, got %q", got)
		}
	}

	// Unknown language code → en fallback (config.NormalizeLanguage rejects it).
	config.CurrentLanguage = func() string { return "fr" }
	if got := trMsg(srcMsg); got != srcMsg {
		t.Fatalf("unknown lang: trMsg must fall back to English, got %q", got)
	}

	// A string not in the catalog (propagated lower-layer error) passes through.
	config.CurrentLanguage = func() string { return "ko" }
	const notInCatalog = "some-lower-layer-error: connection refused"
	if got := trMsg(notInCatalog); got != notInCatalog {
		t.Fatalf("non-catalog string must pass through unchanged, got %q", got)
	}
}
