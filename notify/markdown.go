package notify

import (
	"fmt"
	"strings"
)

// This file holds the message rendering shared by the markdown-family channels
// (DingTalk, WeCom). Feishu uses card JSON, Telegram and email use HTML, each rendered
// in its own adapter.

// maxAssetsShown is the maximum number of assets listed in a message. A finding can be
// anchored to dozens of assets; listing all would blow up the message for no value,
// since nobody reads the fourth domain onward in IM.
const maxAssetsShown = 3

// maxSummaryRunes is how many characters the summary is shortened to. An IM message is
// a prompt to go look at the details, not the report itself; full content is on the platform.
const maxSummaryRunes = 120

// markdownReservedBytes is reserved for the message header (digest line + severity
// breakdown + possible truncation notice) and footer (platform link). Whole-item
// packing subtracts it from the budget so header and footer never get cut; without
// them readers can't tell which batch this is or how many items weren't shown.
const markdownReservedBytes = 320

// markdownEscape escapes markdown metacharacters.
//
// Why it is required: finding titles, summaries, types and asset display names all
// come from **untrusted sources**. Titles and summaries are model output (the model
// read the target's responses), and asset urls are full scanned URLs (including
// target-controlled query strings). Without escaping, a finding titled
//
//	Login SQL injection\n[URGENT: click to verify your account](http://attacker.tld)
//
// would render as a **clickable external link** in a security engineer's
// DingTalk/Feishu, and `![](http://attacker.tld/beacon)` would be fetched by the
// client on render, announcing "this finding has been seen" and leaking the reader's
// IP. Even benign content could inject bold text or quote blocks that push a critical
// finding below the fold.
//
// The escape set covers characters that create headings, links, emphasis, lists,
// quotes and strikethrough, i.e. that change structure or produce clickable elements.
// `\` must be handled first, or the backslashes added afterwards would be escaped again.
func markdownEscape(s string) string {
	replacer := strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"!", `\!`,
		"#", `\#`,
		">", `\>`,
		"|", `\|`,
		"~", `\~`,
	)
	return replacer.Replace(s)
}

// markdownText collapses untrusted text to one line and escapes it for markdown bodies.
// Single-lining is the other half besides escaping: a newline alone can forge a new
// list item or quote block, which escaping characters can't stop.
func markdownText(s string, maxRunes int) string {
	return markdownEscape(OneLine(s, maxRunes))
}

// markdownTitle returns the message title (the IM platform's title bar / card title)
// as **unescaped raw text**.
//
// Deliberately not escaped: this title is shared by renderers in four contexts:
// markdown bodies, Telegram HTML, Feishu card plain_text, and generic webhook JSON
// plus email subjects. Each context has different escaping rules (markdown escaping in
// HTML leaves visible backslashes, in JSON it corrupts the data), so each output must
// do its own escaping; see writeItem / feishuItemLines / telegramEscape. Markdown
// escaping was once added in this shared function, and Telegram messages ended up
// showing visible backslashes like `\(1\)`.
func markdownTitle(m Message) string {
	if m.Batch {
		return fmt.Sprintf("Findings digest · %d total", len(m.Items))
	}
	if len(m.Items) == 0 {
		return "Finding alert"
	}
	it := m.Items[0]
	return fmt.Sprintf("[%s] %s", SeverityLabel(it.Severity), OneLine(it.Title(), 0))
}

// markdownBody renders the message body, returning it and the **number of items
// actually written**.
//
// kept is the number of items this delivery really delivers; the caller marks only
// the first kept items delivered. Items kept out by the channel's length limit must
// wait for the next batch rather than be marked successful too. That is exactly where
// "silent loss" comes from: the message was truncated, the delivery records say
// everything was delivered, and nothing shows the tail was never sent.
//
// maxBytes<=0 means unlimited.
func markdownBody(m Message, maxBytes int) (string, int) {
	if !m.Batch {
		if len(m.Items) == 0 {
			return "", 0
		}
		var b strings.Builder
		writeItem(&b, m.Items[0], "", true)
		// A single-item message is sent even when oversized (final truncation is the
		// backstop): part of a finding is better than nothing.
		return TruncateBytes(b.String(), maxBytes), 1
	}

	footer := ""
	if m.HomeURL != "" {
		footer = fmt.Sprintf("\n[View all on the platform](%s)\n", m.HomeURL)
	}
	kept := packItemCount(m.Items, maxBytes, markdownReservedBytes, footer, byteSize, func(it Item, idx int) string {
		var b strings.Builder
		writeItem(&b, it, fmt.Sprintf("%d. ", idx+1), false)
		return b.String()
	})

	items := m.Items[:kept]
	var b strings.Builder
	b.WriteString(markdownBatchIntro(m, items, len(m.Items)))
	for i, it := range items {
		writeItem(&b, it, fmt.Sprintf("%d. ", i+1), false)
	}
	b.WriteString(footer)
	return TruncateBytes(b.String(), maxBytes), kept
}

// markdownBatchIntro renders the start of a digest: time window, count and severity
// breakdown, so recipients can judge whether the batch needs immediate attention
// without opening the platform.
//
// items are the entries that **actually fit**; total is the batch's full count. When
// they differ we must say how many follow in the next message; otherwise readers take
// the header number as everything, and the unsent items simply don't exist in the UI.
func markdownBatchIntro(m Message, items []Item, total int) string {
	var b strings.Builder
	if m.WindowMinutes > 0 {
		fmt.Fprintf(&b, "**New findings in the last %d minutes: %d**", m.WindowMinutes, total)
	} else {
		fmt.Fprintf(&b, "**New findings: %d**", total)
	}
	if extra := total - len(items); extra > 0 {
		fmt.Fprintf(&b, " (this message shows only the first %d; the remaining %d follow in the next message)", len(items), extra)
	}
	// Show the severity breakdown so readers see at a glance whether anything is
	// critical. Count only items **actually in this message**, so "Critical 3" matches
	// what can be counted below.
	counts := map[string]int{}
	for _, it := range items {
		counts[it.Severity]++
	}
	var parts []string
	for _, sev := range []string{"critical", "high", "medium", "low"} {
		if n := counts[sev]; n > 0 {
			parts = append(parts, fmt.Sprintf("%s %d", SeverityLabel(sev), n))
		}
	}
	if len(parts) > 0 {
		b.WriteString("\n" + strings.Join(parts, " · "))
	}
	b.WriteString("\n\n")
	return b.String()
}

// writeItem renders a single finding entry.
//
// prefix is the digest list number; single=true renders the full version (with
// summary and back link), while digest lists render a one-line summary; otherwise a
// 50-item digest becomes a long document.
//
// All externally sourced content (title/type/assets/summary) goes through
// markdownText: single-line + escaping. The back link is built from the
// admin-configured public_base_url, is not untrusted, and must stay clickable, so it
// is output as is.
func writeItem(b *strings.Builder, it Item, prefix string, single bool) {
	line := fmt.Sprintf("%s**%s · %s**", prefix, SeverityLabel(it.Severity), markdownText(it.Title(), 0))
	if !single {
		// Digest mode: one line, with shortened assets and summary appended.
		var extras []string
		if a := assetLine(it.Assets, maxAssetsShown); a != "" {
			extras = append(extras, markdownText(a, 0))
		}
		if it.Summary != "" {
			extras = append(extras, markdownText(it.Summary, 60))
		}
		if len(extras) > 0 {
			line += " — " + strings.Join(extras, " · ")
		}
		b.WriteString(line + "\n")
		return
	}
	b.WriteString(line + "\n")
	if it.IsStatusChange() {
		fmt.Fprintf(b, "**Status change**: %s → %s\n",
			markdownText(StatusLabel(it.FromStatus), 0), markdownText(StatusLabel(it.ToStatus), 0))
	}
	if it.VulnClass != "" && it.VulnClass != it.Title() {
		fmt.Fprintf(b, "**Type**: %s\n", markdownText(it.VulnClass, 0))
	}
	if a := assetLine(it.Assets, maxAssetsShown); a != "" {
		fmt.Fprintf(b, "**Assets**: %s\n", markdownText(a, 0))
	}
	if it.Summary != "" {
		if s := markdownText(it.Summary, maxSummaryRunes); s != "" {
			fmt.Fprintf(b, "**Summary**: %s\n", s)
		}
	}
	if it.DetailURL != "" {
		fmt.Fprintf(b, "[View details](%s)\n", it.DetailURL)
	}
}
