package notify

import (
	"strings"
	"testing"
)

// This file guards the English source text of the email body / HTML template against
// regressions: it catches html.go labels or email.go validation errors drifting back
// to another language. Helpers hasHan / hasHangul / assertEnglish live in
// notify_localized_test.go (same package).
//
// All data (title, type, assets, summary) is ASCII so the "no CJK anywhere in the
// output" assertion catches regressions in the skeleton labels, not the content.

// TestHTMLItemLabelsLocalized turns on every label branch of a single email body and
// checks the skeleton is English with no CJK.
func TestHTMLItemLabelsLocalized(t *testing.T) {
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
	out := htmlBody(m, 0)
	assertEnglish(t, "htmlBody", out)
	for _, want := range []string{
		"Status change", "Type", "Assets", "Summary", "View details", "View all on the platform",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("email body is missing the %q label:\n%s", want, out)
		}
	}
}

// TestHTMLBatchIntroLocalized checks both branches (with/without time window) of the
// digest header render in English.
func TestHTMLBatchIntroLocalized(t *testing.T) {
	items := []Item{{Name: "a", Severity: "high"}, {Name: "b", Severity: "low"}}

	withWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 30, Items: items})
	assertEnglish(t, "htmlBatchIntro(window)", withWindow)
	if !strings.Contains(withWindow, "New findings in the last 30 minutes: 2") {
		t.Errorf("windowed header should contain %q: %q", "New findings in the last 30 minutes: 2", withWindow)
	}

	noWindow := htmlBatchIntro(Message{Batch: true, WindowMinutes: 0, Items: items})
	assertEnglish(t, "htmlBatchIntro(no window)", noWindow)
	if !strings.Contains(noWindow, "New findings: 2") {
		t.Errorf("header without window should contain %q: %q", "New findings: 2", noWindow)
	}
	if strings.Contains(noWindow, "minutes") {
		t.Errorf("no window, yet 'minutes' appears: %q", noWindow)
	}
}

// TestEmailValidateLocalized checks the four SMTP config validation errors are English
// and say which field is the problem. These messages are shown as is to users
// configuring a notification channel.
func TestEmailValidateLocalized(t *testing.T) {
	cases := []struct {
		name   string
		cfg    map[string]any
		substr string
	}{
		{"missing server address", map[string]any{"port": float64(25), "from": "a@b.c", "to": []any{"d@e.f"}}, "SMTP"},
		{"port out of range", map[string]any{"host": "h"}, "port"},
		{"missing sender", map[string]any{"host": "h", "port": float64(25), "to": []any{"d@e.f"}}, "sender"},
		{"missing recipient", map[string]any{"host": "h", "port": float64(25), "from": "a@b.c"}, "recipient"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := (emailChannel{}).Validate(tc.cfg)
			if err == nil {
				t.Fatalf("validation should fail: %v", tc.cfg)
			}
			assertEnglish(t, "Validate("+tc.name+")", err.Error())
			if !strings.Contains(err.Error(), tc.substr) {
				t.Errorf("error message should contain %q, got %q", tc.substr, err.Error())
			}
		})
	}
}
