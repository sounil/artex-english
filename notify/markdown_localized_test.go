package notify

import (
	"strings"
	"testing"
)

// hanFreeItems builds only CJK-free items (ASCII title, type and summary). Only when
// the data has no CJK can an "entire output is English" assertion catch a label
// regression precisely.
func hanFreeItems(n int) []Item {
	items := make([]Item, 0, n)
	for i := 0; i < n; i++ {
		items = append(items, Item{
			FindingID: int64(i + 1),
			Name:      "login-flaw",
			VulnClass: "SQLi",
			Severity:  "high",
			Summary:   "SQL injection via q param",
			Assets:    []string{"a.example.com"},
		})
	}
	return items
}

// TestMarkdownTitleLocalized checks markdownTitle, shared by the markdown channels
// (dingtalk, wecom) and by feishu, html, telegram and webhook, is English. A regression
// would mix languages in the titles of all of these channels.
func TestMarkdownTitleLocalized(t *testing.T) {
	// Digest: "Findings digest · N total"
	got := markdownTitle(Message{Batch: true, Items: hanFreeItems(3)})
	assertEnglish(t, "markdownTitle(batch)", got)
	if got != "Findings digest · 3 total" {
		t.Errorf("digest title should be 'Findings digest · 3 total', got %q", got)
	}
	// No items: "Finding alert"
	empty := markdownTitle(Message{})
	assertEnglish(t, "markdownTitle(empty)", empty)
	if empty != "Finding alert" {
		t.Errorf("empty message title should be 'Finding alert', got %q", empty)
	}
}

// TestMarkdownBatchIntroLocalized checks the digest header (time window, count,
// overflow notice) is English. Item data has no CJK, so any CJK in the output means
// the header text regressed.
func TestMarkdownBatchIntroLocalized(t *testing.T) {
	items := hanFreeItems(3)

	// With window: "New findings in the last N minutes: N"
	win := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 3)
	assertEnglish(t, "markdownBatchIntro(window)", win)
	if !strings.Contains(win, "New findings in the last 30 minutes: 3") {
		t.Errorf("windowed header should read 'New findings in the last 30 minutes: 3', got %q", win)
	}

	// Without window: "New findings: N" (no minutes)
	noWin := markdownBatchIntro(Message{Batch: true}, items, 3)
	assertEnglish(t, "markdownBatchIntro(no window)", noWin)
	if !strings.Contains(noWin, "New findings: 3") || strings.Contains(noWin, "minutes") {
		t.Errorf("header without window should be 'New findings: 3' (no minutes), got %q", noWin)
	}

	// Partially packed: "(this message shows only the first N; the remaining N follow in the next message)"
	trunc := markdownBatchIntro(Message{Batch: true, WindowMinutes: 30}, items, 5)
	assertEnglish(t, "markdownBatchIntro(overflow)", trunc)
	for _, want := range []string{"only the first 3", "remaining 2", "next message"} {
		if !strings.Contains(trunc, want) {
			t.Errorf("overflow notice should contain %q, got %q", want, trunc)
		}
	}
}

// TestWriteItemLabelsLocalized checks the field labels (status change, type, assets,
// summary) and detail link of the single-item render (the path shared by the markdown
// channels) are English.
func TestWriteItemLabelsLocalized(t *testing.T) {
	it := Item{
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/1",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
	var b strings.Builder
	writeItem(&b, it, "", true)
	got := b.String()

	assertEnglish(t, "writeItem(single)", got)
	for _, want := range []string{
		"**Status change**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Summary**: SQL injection via q param",
		"[View details](https://platform.example/finding/1)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("single render should contain %q:\n%s", want, got)
		}
	}
}

// TestMarkdownBodyFooterLocalized checks the platform link at the end of a digest body
// is English (it is only added when HomeURL is set).
func TestMarkdownBodyFooterLocalized(t *testing.T) {
	m := Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)}
	body, kept := markdownBody(m, 0)
	if kept != 2 {
		t.Fatalf("with no limit (0) both items should fit, got %d", kept)
	}
	if !strings.Contains(body, "[View all on the platform](https://platform.example)") {
		t.Errorf("body should end with a 'View all on the platform' link:\n%s", body)
	}
}
