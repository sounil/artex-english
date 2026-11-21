// Package notify implements the IM / email push-channel adapter layer for findings.
//
// Layering: this is a **leaf package** that depends only on the standard library. It
// knows nothing about the database or the server. Channel configuration comes in as
// map[string]any (the notification_channels.config JSONB column) and the content to
// push comes in as a Message. Splitting it this way lets the genuinely error-prone
// parts (signature computation, UTF-8 truncation, filter matching) be unit-tested
// without PostgreSQL; the host only orchestrates on the server side.
//
// Concurrency contract: Channel implementations must be **stateless**. One Channel
// instance is reused concurrently by many channel configs (even multiple bot
// instances of the same channel); all credentials come in through the cfg argument,
// and implementations must not cache things like the webhook URL in their own fields.
package notify

// Channel kind identifiers. The values are also the valid set for
// notification_channels.kind, allow-listed on the server side (like findings.status,
// no DB CHECK, so new channels are easy to add).
const (
	KindDingTalk = "dingtalk" // DingTalk custom robot
	KindFeishu   = "feishu"   // Feishu (incl. Lark) custom robot
	KindWeCom    = "wecom"    // WeCom group robot
	KindWebhook  = "webhook"  // Generic webhook: custom method/headers/JSON template
	KindTelegram = "telegram" // Telegram Bot API
	KindEmail    = "email"    // SMTP email
)

// Event kinds, matching notification_events.kind.
const (
	EventFindingCreated       = "finding_created"
	EventFindingStatusChanged = "finding_status_changed"
)

// InitKind is the fallback when kind is empty in config.
const InitKind = KindDingTalk

// severityRank maps severities to comparable ordinals. Unknown severities map to 0,
// so any min_severity setting filters them out: when in doubt, don't push, to avoid
// flooding with noise.
var severityRank = map[string]int{
	"low":      1,
	"medium":   2,
	"high":     3,
	"critical": 4,
}

// SeverityRank returns the severity's ordinal; unknown severities return 0.
func SeverityRank(severity string) int { return severityRank[severity] }

// severityLabels / statusLabels hold the display strings used in push and email messages.
var severityLabels = map[string]string{
	"critical": "🔴 Critical", "high": "🟠 High", "medium": "🟡 Medium", "low": "🔵 Low",
}

var statusLabels = map[string]string{
	"pending": "Pending", "in_progress": "In progress", "confirmed": "Confirmed", "resolved": "Resolved",
	"fixed": "Fixed", "false_positive": "False positive", "ignored": "Ignored", "duplicate": "Duplicate",
	"risk_accepted": "Risk accepted",
}

// SeverityLabel returns the emoji-prefixed severity name, used for message titles
// and card colors. Unknown severities are echoed
// back unchanged rather than guessed.
func SeverityLabel(severity string) string {
	if v, ok := severityLabels[severity]; ok {
		return v
	}
	return severity
}

// StatusLabel returns the display name of a triage status for status-change messages. Unknown statuses are echoed back unchanged.
func StatusLabel(status string) string {
	if v, ok := statusLabels[status]; ok {
		return v
	}
	return status
}

// AtLeast reports whether severity meets the min threshold. An empty min means no
// threshold: everything passes. Note an unknown severity has ordinal 0 and is rejected
// by any non-empty min (see the severityRank comment).
func AtLeast(severity, min string) bool {
	if min == "" {
		return true
	}
	return SeverityRank(severity) >= SeverityRank(min)
}
