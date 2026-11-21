package sidequestion

import (
	"errors"
	"fmt"
	"testing"
	"unicode"
)

// assertEnglish fails if msg is empty or carries any Hangul, Han or kana rune:
// the package's user-facing literals are English source text, and CJK there
// means an untranslated leftover.
func assertEnglish(t *testing.T, label, msg string) {
	t.Helper()
	if msg == "" {
		t.Fatalf("%s: empty message", label)
	}
	for _, r := range msg {
		if unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
			t.Fatalf("%s: contains CJK %q: %q", label, string(r), msg)
		}
	}
}

// TestSideQuestionOutputsLocalized guards F21: every user-facing side-question
// error and answer-text literal produced by this package must be English source
// text. These surface through /api/.../side-questions into the chat side-question
// panel, so CJK leftovers here are a user-visible regression.
func TestSideQuestionOutputsLocalized(t *testing.T) {
	assertEnglish(t, "ErrContextBudget", ErrContextBudget.Error())
	assertEnglish(t, "errSideModelInterrupted", errSideModelInterrupted.Error())
	assertEnglish(t, "errSideNoAnswer", errSideNoAnswer.Error())
	assertEnglish(t, "msgSideToolUnavailable", msgSideToolUnavailable)
	assertEnglish(t, "errSideSummaryCallCap", errSideSummaryCallCap.Error())
	assertEnglish(t, "sideSummaryFailedPrefix", sideSummaryFailedPrefix)
	assertEnglish(t, "errSideSummaryIncomplete", errSideSummaryIncomplete.Error())
	assertEnglish(t, "errSideSummaryOverBudget", errSideSummaryOverBudget.Error())
	assertEnglish(t, "errSideHistoryCursor", errSideHistoryCursor.Error())
	assertEnglish(t, "errSideCompactionStalled", errSideCompactionStalled.Error())
}

// TestSideQuestionBudgetSentinelPreserved: localizing the message must not break
// callers that branch on the ErrContextBudget sentinel with errors.Is (see
// context_test.go, which relies on it after the recovery path gives up).
func TestSideQuestionBudgetSentinelPreserved(t *testing.T) {
	if !errors.Is(fmt.Errorf("prepare: %w", ErrContextBudget), ErrContextBudget) {
		t.Fatal("ErrContextBudget sentinel identity (errors.Is) is broken")
	}
}

// TestSideQuestionPromptsEnglish: the model-facing prompts (the answering
// instruction and the summary instruction) are English source text too.
func TestSideQuestionPromptsEnglish(t *testing.T) {
	assertEnglish(t, "instruction", instruction)
	assertEnglish(t, "summaryInstruction", summaryInstruction)
}
