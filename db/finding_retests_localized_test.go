package db

import (
	"testing"
	"unicode"
)

// assertRetestReasonEnglish asserts that a retest reason constant is English source
// text: non-empty, with no Hangul, Han or kana (F9). The constants are spliced into SQL
// literals by FinishFindingRetest and RecoverFindingRetests, so pinning them also
// protects the user-facing text shown in the panel.
func assertRetestReasonEnglish(t *testing.T, name, s string) {
	t.Helper()
	if s == "" {
		t.Fatalf("%s: empty string", name)
	}
	for _, r := range s {
		if unicode.In(r, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana) {
			t.Fatalf("%s: contains non-English (CJK) text: %q", name, s)
		}
	}
}

// TestFindingRetestReasonsLocalized asserts that the two termination reasons stored in
// the finding_retests.error column (and shown as item.error in the retest panel,
// finding-retest-panel) are English source text. They share the column and panel with
// the sibling reasons (convRetest*) in server/conversations.go, so if only one side is
// English the panel mixes languages.
func TestFindingRetestReasonsLocalized(t *testing.T) {
	assertRetestReasonEnglish(t, "retestNoConclusionReason", retestNoConclusionReason)
	assertRetestReasonEnglish(t, "retestServiceRestartReason", retestServiceRestartReason)
}
