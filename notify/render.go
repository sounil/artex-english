package notify

import (
	"strings"
	"unicode/utf8"
)

const ellipsis = "…"

// TruncateBytes truncates s to at most max bytes, guaranteeing the result is valid
// UTF-8 and never splits a character.
//
// Why it must cut on character boundaries: WeCom group-robot markdown has a hard
// 4096-**byte** limit (not characters), and one CJK character is 3 bytes. Slicing by
// bytes can split a character in half and produce invalid UTF-8; the platform then
// either rejects the whole message or renders mojibake. So we back off from the
// budget position to the nearest rune start byte (utf8.RuneStart detects
// continuation bytes 0b10xxxxxx).
//
// max<=0 means unlimited. An ellipsis is appended after truncation unless max is too
// small to fit it.
func TruncateBytes(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	budget := max - len(ellipsis)
	suffix := ellipsis
	if budget < 0 {
		// max is shorter than the ellipsis: drop it and just cut, so the result never exceeds max.
		budget = max
		suffix = ""
	}
	cut := budget
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + suffix
}

// OneLine collapses multi-line text into one line: fold all whitespace, then truncate
// by character count. Used for IM title lines: summaries often contain newlines that
// would break table/title layout. max<=0 means unlimited.
func OneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	return TruncateRunes(s, max)
}

// TruncateRunes truncates s to at most max characters (not bytes), appending an
// ellipsis when it cuts. max<=0 means unlimited.
//
// The difference from TruncateBytes is the platform's unit: WeCom limits by bytes,
// Telegram by characters. Using the wrong unit does not error, it just cuts messages
// far shorter than expected (one CJK character = 3 bytes, so 4096 bytes is only about
// 1365 characters). Both functions must stay and be chosen per channel.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max <= 1 {
		return string(runes[:max])
	}
	return string(runes[:max-1]) + ellipsis
}

// TruncateHTML truncates an HTML fragment by character count without leaving a
// half-written tag.
//
// Cutting HTML by characters can produce a broken tag like `<a href="htt`; the
// platform parser then either rejects the whole message or swallows the following
// text as an attribute value. So we cut by characters first, then check for an
// unclosed trailing `<` and back off to before it.
//
// Tags are not rebalanced (adding </b> etc.): Telegram's HTML parser closes unclosed
// tags itself, and rebalancing would mean handling quotes in attributes, comments and
// self-closing tags, which is not worth the complexity.
func TruncateHTML(s string, max int) string {
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	cut := TruncateRunes(s, max)
	// If the tail is a fragment starting with `<` (no `>` after the last `<`), back off to before it.
	if lt := strings.LastIndex(cut, "<"); lt >= 0 && !strings.Contains(cut[lt:], ">") {
		cut = cut[:lt]
	}
	// Likewise back off if the tail is a cut HTML entity (e.g. `&amp;` cut to `&amp`).
	// An entity fragment can make a strict parser reject the **whole message**; digest
	// messages over the length limit are common, and not worth losing a notification over.
	if amp := strings.LastIndex(cut, "&"); amp >= 0 && !strings.Contains(cut[amp:], ";") {
		cut = cut[:amp]
	}
	return cut
}

// packItemCount computes how many items fit **whole** within the budget, so digest
// messages are packed by whole items.
//
// Why pack whole items instead of rendering everything and truncating: truncation
// makes the trailing items silently disappear while their delivery records are still
// marked delivered. It shows neither in the message nor in the delivery history, and
// the findings are simply lost. With whole-item packing, items that don't fit stay in
// the store for the next batch, and the kept count the caller gets is exactly how
// many this message really delivered.
//
// Parameters: maxSize<=0 means unlimited; reserve is the amount set aside for the
// header/footer; size does the measuring (platforms differ: WeCom/DingTalk count
// bytes, Telegram counts characters; using the wrong one does not error, it just
// squeezes CJK messages far below the limit); render renders item idx into its actual
// text, whose length varies with content and cannot be estimated.
//
// Returns at least 1 (as long as there are items). Even an extremely long single item
// must be sent, with the caller's final truncation as the backstop; otherwise one
// oversized finding would block the whole batch forever.
func packItemCount(items []Item, maxSize, reserve int, footer string, size func(string) int, render func(Item, int) string) int {
	if maxSize <= 0 {
		return len(items)
	}
	budget := maxSize - reserve - size(footer)
	if budget < 0 {
		budget = 0
	}
	used := 0
	for i, it := range items {
		used += size(render(it, i))
		if used > budget && i > 0 {
			return i
		}
	}
	return len(items)
}

// byteSize / runeSize are packItemCount's two units of measure, named so call sites
// don't use bare func(s string) int closures where the unit isn't obvious at a glance.
func byteSize(s string) int { return len(s) }
func runeSize(s string) int { return utf8.RuneCountInString(s) }

// assetLine renders the asset list as one display line; beyond limit entries the rest
// are elided and the total is shown. One finding can be anchored to dozens of assets,
// and listing them all would blow up the message.
func assetLine(assets []string, limit int) string {
	if len(assets) == 0 {
		return ""
	}
	if limit <= 0 || len(assets) <= limit {
		return strings.Join(assets, ", ")
	}
	return strings.Join(assets[:limit], ", ") + ", … (" + itoa(len(assets)) + " total)"
}

// itoa is a short stand-in for strconv.Itoa, used only for building display text
// so strconv need not be imported everywhere.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
