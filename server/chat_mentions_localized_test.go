package server

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestChatMentionErrorsLocalized guards F3b chat_mentions.go: every user-facing
// @mention (citation) error response must be English (no Hangul, Han, or kana). The
// wire token labels (chatMentionPattern, chatMentionKinds) are a format shared with
// the web client and stay CJK by design; they, the agent-input snapshot header and the
// truncation markers fed into that snapshot are intentionally not checked here.
func TestChatMentionErrorsLocalized(t *testing.T) {
	// Fixed user-facing literals.
	for label, msg := range map[string]string{
		"badID":       errChatMentionBadID,
		"tooMany":     errChatMentionTooMany,
		"badSearch":   errChatMentionBadSearch,
		"dataUnavail": errChatMentionDataUnavail,
		"tooLarge":    errChatMentionTooLarge,
	} {
		assertEnglishError(t, label, msg)
	}

	// parseChatMentions — a non-positive id inside a valid wire token.
	if _, err := parseChatMentions("@[漏洞#0]"); err == nil {
		t.Fatal("an invalid citation ID must not pass")
	} else {
		assertEnglishError(t, "parse.badID", err.Error())
	}

	// parseChatMentions — exceeding the per-message mention cap (11 distinct ids).
	var b strings.Builder
	for i := 1; i <= 11; i++ {
		fmt.Fprintf(&b, "@[漏洞#%d] ", i)
	}
	if _, err := parseChatMentions(b.String()); err == nil {
		t.Fatal("exceeding the citation cap must not pass")
	} else {
		assertEnglishError(t, "parse.tooMany", err.Error())
	}

	// composeChatMentionMessage — a mention is present but the database is nil.
	if _, err := composeChatMentionMessage(nil, "@[漏洞#1]"); err == nil {
		t.Fatal("citation resolution must not pass without a DB")
	} else {
		assertEnglishError(t, "compose.dataUnavail", err.Error())
	}

	// searchChatMentions — invalid kind and over-long query are rejected before
	// the database handle is touched, so &Server{} is enough to reach the branch.
	for _, q := range []string{"kind=unsupported", "q=" + url.QueryEscape(strings.Repeat("字", 201))} {
		w := httptest.NewRecorder()
		(&Server{}).searchChatMentions(w, httptest.NewRequest("GET", "/api/chat/mentions?"+q, nil))
		if w.Code != 400 {
			t.Fatalf("expected a validation failure (400), got %d (%s)", w.Code, q)
		}
		assertEnglishError(t, "search."+q, decodeErrorField(t, w.Body.Bytes()))
	}

	// The display label map mirrors the UI mention kinds, and the not-found
	// template resolves to a fully English message for a known kind.
	for _, kind := range []string{"finding", "asset", "company", "endpoint", "ip", "app", "root_domain", "subdomain", "service"} {
		if chatMentionKindLabel[kind] == "" {
			t.Fatalf("kind label is missing: %s", kind)
		}
	}
	assertEnglishError(t, "notFound", fmt.Sprintf(errChatMentionNotFoundFmt, chatMentionKindLabel["finding"], 7))
}
