package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep the error responses of conversations.go's conversation
// (chat) API in English. The English check reuses assertEnglishError (no Hangul, Han,
// or kana).

// TestConversationErrorConstantsLocalized asserts that all 12 response constants are
// English. The handlers go through s.pg(w) (DB) first and cannot run end to end on a
// DB-less host, so the constants themselves are asserted (following the auth.go
// precedent). Format strings containing %d are filled with real args and the final
// message is checked.
func TestConversationErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"request_too_large", convErrRequestTooLarge},
		{"agent_key_empty", convErrAgentKeyEmpty},
		{"agent_key_too_long", fmt.Sprintf(convErrAgentKeyTooLong, maxConversationAgentKeyRunes)},
		{"agent_not_found", convErrAgentNotFound},
		{"llm_profile", convErrLLMProfile},
		{"title_too_long", fmt.Sprintf(convErrTitleTooLong, maxConversationTitleRunes)},
		{"title_or_pinned", convErrTitleOrPinned},
		{"title_empty", convErrTitleEmpty},
		{"bad_conv_id", convErrBadConvID},
		{"ids_count", fmt.Sprintf(convErrIDsCount, maxConversationDeleteBatch)},
		{"message_empty", convErrMessageEmpty},
		{"busy", convErrBusy},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// TestDecodeConversationRequestTooLargeLocalized checks the body-too-large path all the
// way to the real HTTP response body. decodeConversationRequest is a package function
// that does not go through Server (DB), so it runs without a DB, and it confirms the
// constant actually reaches the response (body too large → 413 + English message).
func TestDecodeConversationRequestTooLargeLocalized(t *testing.T) {
	// Valid JSON body over the 64KB limit. MaxBytesReader reports the overflow mid-read.
	body := `{"title":"` + strings.Repeat("a", maxConversationRequestBytes+1024) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/conversations", strings.NewReader(body))
	rec := httptest.NewRecorder()
	var dst struct {
		Title string `json:"title"`
	}
	if decodeConversationRequest(rec, req, &dst) {
		t.Fatal("decodeConversationRequest returned true although the body exceeded the limit")
	}
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status code = %d, want %d", rec.Code, http.StatusRequestEntityTooLarge)
	}
	var resp struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to parse response JSON: %v (body=%q)", err, rec.Body.String())
	}
	if resp.Error != convErrRequestTooLarge {
		t.Fatalf("response error = %q, want %q", resp.Error, convErrRequestTooLarge)
	}
	assertEnglishError(t, "decode.too_large.response", resp.Error)
}

// TestConversationDefaultTitlesLocalized asserts that the two default conversation
// title constants (F8) are English and distinct. convDefaultTitle is both the creation
// default and the sentinel of the auto-title branch, so reverting it to Korean or
// Chinese would show that language in the conversation list and delete dialog.
func TestConversationDefaultTitlesLocalized(t *testing.T) {
	assertEnglishError(t, "default_title", convDefaultTitle)
	assertEnglishError(t, "attachment_title", convAttachmentTitle)
	if convDefaultTitle == convAttachmentTitle {
		t.Fatal("the default title and the attachment default title must differ")
	}
}

// TestIsDefaultConversationTitle pins the auto-title branch decision. The creation
// default (convDefaultTitle), the legacy Korean/Chinese defaults still stored in
// existing databases, and an empty title get an auto title; a user-chosen title does
// not. The creation default and the sentinel are the same constant, which prevents a
// regression where they drift apart and no auto title is applied.
func TestIsDefaultConversationTitle(t *testing.T) {
	if !isDefaultConversationTitle("") {
		t.Fatal("an empty title must get an auto title")
	}
	if !isDefaultConversationTitle(convDefaultTitle) {
		t.Fatalf("the creation default %q must get an auto title", convDefaultTitle)
	}
	// legacy: default titles stored by earlier Korean/Chinese releases.
	for _, legacy := range []string{"새 대화", "新对话"} {
		if !isDefaultConversationTitle(legacy) {
			t.Fatalf("the legacy default %q must get an auto title", legacy)
		}
	}
	if isDefaultConversationTitle("A title the user chose") {
		t.Fatal("a user-chosen title must not get an auto title")
	}
}

// TestConversationRetestReasonsLocalized asserts that the three retest termination
// reason constants (F9) are English. These values are stored in the
// finding_retests.error column and shown as item.error in the retest panel, so
// reverting them would show Korean or Chinese reasons in the panel.
func TestConversationRetestReasonsLocalized(t *testing.T) {
	assertEnglishError(t, "retest_failed_to_start", convRetestFailedToStart)
	assertEnglishError(t, "retest_status_read_failed", convRetestStatusReadFailed)
	assertEnglishError(t, "retest_stopped_or_closed", convRetestStoppedOrClosed)
}

// TestTranscriptErrorSummaryLocalized pins the activity transcript error wrapper (F9).
// Both callers — the chat turn (no label) and the task main agent ("main agent") —
// produce the same "(... error: ...)" shape, the inner err text is preserved verbatim,
// and the wrapper must carry no CJK characters or full-width punctuation.
func TestTranscriptErrorSummaryLocalized(t *testing.T) {
	cases := []struct {
		name  string
		label string
		errm  string
		want  string
	}{
		{"chat_turn", "", "connection reset", "(error: connection reset)"},
		{"main_agent", "main agent", "connection reset", "(main agent error: connection reset)"},
	}
	for _, c := range cases {
		got := transcriptErrorSummary(c.label, c.errm)
		if got != c.want {
			t.Fatalf("%s: transcriptErrorSummary = %q, want %q", c.name, got, c.want)
		}
		if !strings.Contains(got, c.errm) {
			t.Fatalf("%s: the err text was not preserved: %q", c.name, got)
		}
		// Confirm full-width parentheses/colons were replaced with ASCII (F1 policy).
		if strings.ContainsAny(got, "（）：") {
			t.Fatalf("%s: full-width punctuation remains: %q", c.name, got)
		}
		// The wrapper label must be English (the err text is not checked and is fixed to
		// ASCII). The chat turn without a label also includes "error".
		wrapper := strings.ReplaceAll(got, c.errm, "")
		assertEnglishError(t, c.name+".wrapper", wrapper)
	}
}
