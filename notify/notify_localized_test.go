package notify

import (
	"strings"
	"testing"
)

// hasHan reports whether s contains any CJK unified ideograph.
func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// hasHangul reports whether s contains any Hangul syllable.
func hasHangul(s string) bool {
	for _, r := range s {
		if r >= 0xAC00 && r <= 0xD7A3 {
			return true
		}
	}
	return false
}

// hasKana reports whether s contains any Hiragana or Katakana.
func hasKana(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30FF {
			return true
		}
	}
	return false
}

// assertEnglish asserts the source (default-language) output is English: no Hangul,
// no Han and no kana. Rendered text that embeds deliberately CJK test data must not
// be passed here.
func assertEnglish(t *testing.T, where, got string) {
	t.Helper()
	if hasHangul(got) {
		t.Errorf("%s: Hangul left in source output: %q", where, got)
	}
	if hasHan(got) {
		t.Errorf("%s: Han characters left in source output: %q", where, got)
	}
	if hasKana(got) {
		t.Errorf("%s: kana left in source output: %q", where, got)
	}
}

// withLang sets the injected output language for the duration of a test.
func withLang(t *testing.T, lang string) {
	t.Helper()
	prev := Lang
	Lang = func() string { return lang }
	t.Cleanup(func() { Lang = prev })
}

// TestSeverityLabelLocalized checks every severity enum renders an English label by
// default and the Korean translation when the language is ko. Labels are shared by
// every notification channel, so a regression mixes languages across all messages.
func TestSeverityLabelLocalized(t *testing.T) {
	want := map[string]string{
		"critical": "Critical",
		"high":     "High",
		"medium":   "Medium",
		"low":      "Low",
	}
	for sev, label := range want {
		got := SeverityLabel(sev)
		assertEnglish(t, "SeverityLabel("+sev+")", got)
		if !strings.Contains(got, label) {
			t.Errorf("SeverityLabel(%q)=%q, should contain %q", sev, got, label)
		}
	}
	// Unknown severities are echoed back unchanged (never invented).
	if got := SeverityLabel("made_up"); got != "made_up" {
		t.Errorf("unknown severity should be kept as is, got %q", got)
	}

	// language=ko → Korean translation table.
	withLang(t, "ko")
	koWant := map[string]string{
		"critical": "심각",
		"high":     "높음",
		"medium":   "중간",
		"low":      "낮음",
	}
	for sev, label := range koWant {
		if got := SeverityLabel(sev); !strings.Contains(got, label) {
			t.Errorf("ko SeverityLabel(%q)=%q, should contain %q", sev, got, label)
		}
	}
}

// TestStatusLabelLocalized checks all nine triage statuses render in English by
// default and match the Korean translation when the language is ko.
func TestStatusLabelLocalized(t *testing.T) {
	want := map[string]string{
		"pending":        "Pending",
		"in_progress":    "In progress",
		"confirmed":      "Confirmed",
		"resolved":       "Resolved",
		"fixed":          "Fixed",
		"false_positive": "False positive",
		"ignored":        "Ignored",
		"duplicate":      "Duplicate",
		"risk_accepted":  "Risk accepted",
	}
	for status, label := range want {
		got := StatusLabel(status)
		assertEnglish(t, "StatusLabel("+status+")", got)
		if got != label {
			t.Errorf("StatusLabel(%q)=%q, want %q", status, got, label)
		}
	}
	// Unknown statuses are echoed back unchanged (never invented).
	if got := StatusLabel("weird_status"); got != "weird_status" {
		t.Errorf("unknown status should be kept as is, got %q", got)
	}

	// language=ko → Korean translation table (must match the UI status.finding namespace).
	withLang(t, "ko")
	koWant := map[string]string{
		"pending":        "처리 대기",
		"in_progress":    "처리 중",
		"confirmed":      "확인됨",
		"resolved":       "처리됨",
		"fixed":          "수정됨",
		"false_positive": "오탐",
		"ignored":        "무시",
		"duplicate":      "중복",
		"risk_accepted":  "위험 수용",
	}
	for status, label := range koWant {
		if got := StatusLabel(status); got != label {
			t.Errorf("ko StatusLabel(%q)=%q, want %q", status, got, label)
		}
	}
}

// TestItemTitlePlaceholderLocalized checks that an item with neither name nor type
// falls back to an English placeholder; a blank title is never produced.
func TestItemTitlePlaceholderLocalized(t *testing.T) {
	got := Item{}.Title()
	assertEnglish(t, "Item{}.Title()", got)
	if got == "" {
		t.Error("placeholder title must not be empty")
	}
	// A name wins; with only a type, the type is used (fallback logic not regressed).
	if n := (Item{Name: "login SQLi"}).Title(); n != "login SQLi" {
		t.Errorf("name-first logic broken, got %q", n)
	}
	if v := (Item{VulnClass: "XSS"}).Title(); v != "XSS" {
		t.Errorf("type fallback logic broken, got %q", v)
	}
}

// TestAssetLineLocalized checks the asset list separator and total-count notation are
// English/ASCII.
func TestAssetLineLocalized(t *testing.T) {
	// Within the limit: list everything, ASCII comma separator.
	if got := assetLine([]string{"a.example.com", "b.example.com"}, 3); got != "a.example.com, b.example.com" {
		t.Errorf("separator should be an ASCII comma, got %q", got)
	}
	// Over the limit: first limit entries + "(N total)" where N is the full count.
	got := assetLine([]string{"a", "b", "c", "d", "e"}, 2)
	assertEnglish(t, "assetLine", got)
	if !strings.Contains(got, "5 total") {
		t.Errorf("full count 5 should be shown as '5 total', got %q", got)
	}
}
