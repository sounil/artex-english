package intercept

import (
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/Autumn-27/artex/config"
)

// hasCJK reports whether s contains any Hangul, Han or kana character.
func hasCJK(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) || unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) {
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

// assertEnglish fails if s is empty or carries any Hangul, Han or kana.
func assertEnglish(t *testing.T, label, s string) {
	t.Helper()
	if strings.TrimSpace(s) == "" {
		t.Errorf("%s: empty message", label)
	}
	if hasCJK(s) {
		t.Errorf("%s: source message is not English: %q", label, s)
	}
}

// withLanguage pins config.ActiveLanguage for the duration of a test.
func withLanguage(t *testing.T, lang string) {
	t.Helper()
	prev := config.CurrentLanguage
	config.CurrentLanguage = func() string { return lang }
	t.Cleanup(func() { config.CurrentLanguage = prev })
}

// TestInterceptMessagesEnglish pins the user-facing judge/approval source messages
// to English. These surface in the approval record reason, the activity stream, and
// the 409 response for an already-decided request.
func TestInterceptMessagesEnglish(t *testing.T) {
	for _, c := range []struct{ name, s string }{
		{"msgReviewContextIncomplete", msgReviewContextIncomplete},
		{"msgModelApprovalFailed", msgModelApprovalFailed},
		{"msgModelOutputUnparsable", msgModelOutputUnparsable},
		{"reasonWorkCanceled", reasonWorkCanceled},
		{"reasonWorkCanceledPreExec", reasonWorkCanceledPreExec},
		{"reasonApprovalTimeout", reasonApprovalTimeout},
		{"reasonManualDeny", reasonManualDeny},
		{"reasonManualAllow", reasonManualAllow},
		{"ErrAlreadyDecided", ErrAlreadyDecided.Error()},
	} {
		assertEnglish(t, c.name, c.s)
	}
}

// TestJudgeActionLabelLocalized checks the three judge verdict labels follow the
// active language (English by default, Korean 허용 / 차단 / 확인 요청 when ko is
// selected) and that an unknown action still passes through untranslated.
func TestJudgeActionLabelLocalized(t *testing.T) {
	withLanguage(t, "en")
	for _, action := range []string{"allow", "deny", "ask"} {
		assertEnglish(t, "judgeActionLabel("+action+")", judgeActionLabel(action))
	}
	if got := judgeActionLabel("weird"); got != "weird" {
		t.Errorf("judgeActionLabel(weird) = %q, want passthrough", got)
	}
	withLanguage(t, "ko")
	want := map[string]string{"allow": "허용", "deny": "차단", "ask": "확인 요청"}
	for action, w := range want {
		if got := judgeActionLabel(action); got != w {
			t.Errorf("ko judgeActionLabel(%s) = %q, want %q", action, got, w)
		}
	}
}

// TestDefaultMessageEnglish checks the rule-derived deny/ask messages are English
// and still embed the rule name, while allow stays empty.
func TestDefaultMessageEnglish(t *testing.T) {
	for _, action := range []string{"deny", "ask"} {
		msg := defaultMessage(action, "R1")
		assertEnglish(t, "defaultMessage("+action+")", msg)
		if !strings.Contains(msg, "R1") {
			t.Errorf("defaultMessage(%s) dropped rule name: %q", action, msg)
		}
	}
	if got := defaultMessage("allow", "R1"); got != "" {
		t.Errorf("defaultMessage(allow) = %q, want empty", got)
	}
}

// TestToolApprovalSummaryShape checks the activity summary stays parseable by
// transcript.tsx, which extracts the pending id via /\(#(\d+)\)/ and the tool name
// via /Approval request for tool\s+(\S+)/.
func TestToolApprovalSummaryShape(t *testing.T) {
	s := fmt.Sprintf(msgToolApprovalRequestFmt, "Bash", 42)
	if !strings.Contains(s, "(#42)") {
		t.Errorf("summary lost the (#N) marker (transcript.tsx pending_id regex): %q", s)
	}
	if !strings.Contains(s, "Approval request for tool Bash ") {
		t.Errorf("summary lost the 'Approval request for tool X' shape (transcript.tsx toolName regex): %q", s)
	}
}
