package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// This file guards the English source text of telegram, webhook and the shared HTTP
// transport layer against regressions: telegram.go message labels and digest titles,
// webhook.go validation errors, http.go transport errors, and validateHTTPURL shared
// by five channels. Helpers hasHan / hasHangul / assertEnglish live in
// notify_localized_test.go (same package).
//
// All data (title, type, assets, summary) is ASCII so the "no CJK anywhere in the
// output" assertion catches regressions in the skeleton labels, not the content.

// TestTelegramItemLabelsLocalized turns on every label branch of a single Telegram
// message and checks the skeleton is English with no CJK.
func TestTelegramItemLabelsLocalized(t *testing.T) {
	m := Message{
		HomeURL: "https://example.com/panel",
		Items: []Item{{
			Name:       "sqli-login", // Title() = Name
			VulnClass:  "injection",  // must differ from Title() for the type line to render
			Severity:   "high",       // SeverityLabel → High
			Summary:    "login form is injectable",
			Assets:     []string{"host-a.example.com"},
			DetailURL:  "https://example.com/f/1",
			FromStatus: "pending", // IsStatusChange()=true → status change line
			ToStatus:   "fixed",
		}},
	}
	out, kept := telegramHTML(m)
	if kept != 1 {
		t.Fatalf("a single message should report 1, got %d", kept)
	}
	assertEnglish(t, "telegramHTML", out)
	for _, want := range []string{"Status change", "Type", "Assets", "Summary", "View details"} {
		if !strings.Contains(out, want) {
			t.Errorf("Telegram body is missing the %q label:\n%s", want, out)
		}
	}
}

// TestTelegramBatchTitleLocalized checks the overflow and time-window branches of the
// digest title render in English, matching markdownTitle's wording so channels don't
// diverge.
func TestTelegramBatchTitleLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	// Overflow + window: digest, total, first N, remaining, next message, last N minutes.
	over := telegramBatchTitle(Message{WindowMinutes: 30}, items, 5)
	assertEnglish(t, "telegramBatchTitle(overflow)", over)
	for _, want := range []string{"Findings digest · 5 total", "showing the first 2", "remaining 3", "next message", "Last 30 minutes"} {
		if !strings.Contains(over, want) {
			t.Errorf("overflow title is missing %q: %q", want, over)
		}
	}

	// No overflow, no window: there must be no next-message notice.
	full := telegramBatchTitle(Message{}, items, 2)
	assertEnglish(t, "telegramBatchTitle(full)", full)
	if full != "Findings digest · 2 total" {
		t.Errorf("full title should be 'Findings digest · 2 total': %q", full)
	}
	if strings.Contains(full, "next message") {
		t.Errorf("no overflow, yet a 'next message' notice appears: %q", full)
	}
}

// TestTelegramValidateLocalized checks missing-field errors are English and say which
// field is empty.
func TestTelegramValidateLocalized(t *testing.T) {
	if err := (telegramChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("missing bot_token should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(bot_token)", err.Error())
		if !strings.Contains(err.Error(), "bot token") {
			t.Errorf("should report the missing bot token: %q", err.Error())
		}
	}
	if err := (telegramChannel{}).Validate(map[string]any{"bot_token": "t"}); err == nil {
		t.Fatal("missing chat_id should fail validation")
	} else {
		assertEnglish(t, "telegram Validate(chat_id)", err.Error())
		if !strings.Contains(err.Error(), "chat ID") {
			t.Errorf("should report the missing chat ID: %q", err.Error())
		}
	}
}

// TestWebhookValidateLocalized checks the three generic webhook validation errors are
// English and explain the cause.
func TestWebhookValidateLocalized(t *testing.T) {
	if err := (webhookChannel{}).Validate(map[string]any{}); err == nil {
		t.Fatal("missing url should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(url)", err.Error())
		if !strings.Contains(err.Error(), "target URL") {
			t.Errorf("should report the missing target URL: %q", err.Error())
		}
	}
	// Unsupported method: English + the allowed list.
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "method": "DELETE"}); err == nil {
		t.Fatal("DELETE should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(method)", err.Error())
		if !strings.Contains(err.Error(), "GET/POST/PUT/PATCH") {
			t.Errorf("should list the allowed methods: %q", err.Error())
		}
	}
	// Template syntax error (the wrapped cause is a Go template error with no CJK).
	if err := (webhookChannel{}).Validate(map[string]any{"url": "https://example.com/hook", "body_template": "{{"}); err == nil {
		t.Fatal("a broken template should fail validation")
	} else {
		assertEnglish(t, "webhook Validate(template)", err.Error())
		if !strings.Contains(err.Error(), "template") {
			t.Errorf("should say it is a template error: %q", err.Error())
		}
	}
}

// TestValidateHTTPURLLocalized checks errors from the URL validation helper shared by
// five channels (telegram, webhook, dingtalk, feishu, wecom) are English, since they
// get wrapped by each channel's prefix ("invalid API URL: ...").
func TestValidateHTTPURLLocalized(t *testing.T) {
	// Unsupported scheme.
	if err := validateHTTPURL("ftp://example.com/x"); err == nil {
		t.Fatal("ftp scheme should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(scheme)", err.Error())
		if !strings.Contains(err.Error(), "http") {
			t.Errorf("should name the supported schemes (http/https): %q", err.Error())
		}
	}
	// No host name.
	if err := validateHTTPURL("http://"); err == nil {
		t.Fatal("a URL without host should be rejected")
	} else {
		assertEnglish(t, "validateHTTPURL(host)", err.Error())
	}
}

// TestHTTPLocalTargetBlockedLocalized checks the local/link-local block error is
// English and names the escape hatch (environment variable).
func TestHTTPLocalTargetBlockedLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "") // explicitly off to take the blocking path
	err := blockInternalDial("tcp", "127.0.0.1:25", nil)
	if err == nil {
		t.Fatal("local addresses should be blocked by default")
	}
	assertEnglish(t, "blockInternalDial", err.Error())
	if !strings.Contains(err.Error(), AllowLocalTargetsEnv) {
		t.Errorf("block error should name the escape hatch (%s): %q", AllowLocalTargetsEnv, err.Error())
	}
}

// TestHTTPRedactHelpersLocalized checks the redaction placeholder and the "unknown
// error" branch of transport errors are English.
func TestHTTPRedactHelpersLocalized(t *testing.T) {
	if got := redactRequestTarget(""); got != "(unparseable address)" {
		t.Errorf("an unparseable address should yield the English placeholder, got %q", got)
	}
	// Branch where *url.Error's Err is nil.
	got := redactTransportError(&url.Error{Op: "Get", URL: "http://api.example", Err: nil})
	assertEnglish(t, "redactTransportError", got)
	if !strings.Contains(got, "unknown error") {
		t.Errorf("a nil Err should yield 'unknown error', got %q", got)
	}
}

// TestHTTPStatusErrorsLocalized checks, via a real doJSON round trip, that the
// user-facing errors of the status classification (rejected / server error / limited)
// are English. Sending to a 127.0.0.1 httptest server is blocked by default, so it is
// explicitly allowed for this test only.
func TestHTTPStatusErrorsLocalized(t *testing.T) {
	t.Setenv(AllowLocalTargetsEnv, "1")
	cases := []struct {
		code int
		want string
	}{
		{http.StatusForbidden, "rejected the request"},
		{http.StatusInternalServerError, "remote server error"},
		{http.StatusTooManyRequests, "rate limited or timed out"},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(tc.code)
			_, _ = w.Write([]byte("body"))
		}))
		_, err := doJSON(context.Background(), http.MethodPost, srv.URL, nil, map[string]any{"a": 1})
		srv.Close()
		if err == nil {
			t.Fatalf("HTTP %d should be an error", tc.code)
		}
		assertEnglish(t, "doJSON(HTTP status)", err.Error())
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("HTTP %d error should contain %q, got %q", tc.code, tc.want, err.Error())
		}
	}
}
