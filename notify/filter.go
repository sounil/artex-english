package notify

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// Filter is the contract for the notification_channels.filter JSONB column: a channel
// instance's filter conditions. All fields are optional and default to "no filtering",
// which is also the fallback for malformed config; see ParseFilter.
type Filter struct {
	// MinSeverity is the minimum severity threshold (low/medium/high/critical); empty = no threshold.
	MinSeverity string `json:"min_severity"`
	// Empty TaskIDs / AssetIDs means unrestricted; otherwise the event must intersect them.
	TaskIDs  []int64 `json:"task_ids"`
	AssetIDs []int64 `json:"asset_ids"`
	// Empty VulnClassInclude accepts everything; otherwise vulnclass must match one of
	// its keywords. Matching any VulnClassExclude keyword excludes (exclude beats include).
	// Matching is case-insensitive substring, safer than regex: a user's bad regex
	// can't silently disable the channel.
	VulnClassInclude []string `json:"vulnclass_include"`
	VulnClassExclude []string `json:"vulnclass_exclude"`
	// OnStatusChange decides whether the channel receives finding status-change events
	// (meaningful only in realtime mode).
	OnStatusChange bool `json:"on_status_change"`
}

// ParseFilter parses a channel's filter config.
//
// **It never returns an error.** This is deliberate: a malformed filter degrades to the
// zero Filter (= no filtering = everything matches), because for a vulnerability
// notification system **one extra push is far better than silently missing a
// high-severity finding**. Turning a parse failure into "don't push" would give users
// a channel that looks configured but never sends anything, the worst failure mode.
func ParseFilter(raw []byte) Filter {
	var f Filter
	if len(raw) == 0 {
		return f
	}
	// On parse failure f stays zero, i.e. no filtering.
	_ = json.Unmarshal(raw, &f)
	return f
}

// ValidMinSeverity reports whether s is a valid severity threshold (empty means none).
func ValidMinSeverity(s string) bool {
	if s == "" {
		return true
	}
	_, ok := severityRank[s]
	return ok
}

// Validate checks the **enumerated** fields of a filter config, called when a channel
// is saved.
//
// Why reject at write time: Match evaluates an unknown threshold as `rank >= 0`, which
// is always true, so a typo in min_severity ("hgih") **silently disables** the filter
// and pushes everything. That matches this package's "over-push rather than miss"
// stance (nothing is missed), but users think they have severity-tiered pushes while
// every finding floods the group, with no hint they misconfigured it. Such silent
// degradation should be stopped at the entry point.
//
// Validate is only for the **write** path. The read path keeps ParseFilter's lenient
// semantics so bad values already in historical data don't make a channel unreadable.
func (f Filter) Validate() error {
	if !ValidMinSeverity(f.MinSeverity) {
		return fmt.Errorf("invalid minimum severity: %q. Choose low / medium / high / critical, or leave it empty for no limit", f.MinSeverity)
	}
	return nil
}

// Match decides whether an event should be delivered to a channel with this filter.
//
// **It never returns an error**, for the same reason as ParseFilter: any internal
// anomaly counts as a match. Order: event kind → severity threshold → task/asset
// scope → vulnclass keywords.
func Match(f Filter, s Snapshot) bool {
	// Only channels that opt in receive status-change events. Off by default, since most
	// users expect "push" to mean "new finding", not a running log of every transition.
	if s.Kind == EventFindingStatusChanged && !f.OnStatusChange {
		return false
	}
	if !AtLeast(s.Severity, f.MinSeverity) {
		return false
	}
	if len(f.TaskIDs) > 0 && !slices.Contains(f.TaskIDs, s.TaskID) {
		return false
	}
	if len(f.AssetIDs) > 0 && !intersectsInt(f.AssetIDs, s.AssetIDs) {
		return false
	}
	// Exclude wins: matching any exclude keyword rejects, even if the include list also matches.
	if len(f.VulnClassExclude) > 0 && containsAnyFold(s.VulnClass, f.VulnClassExclude) {
		return false
	}
	if len(f.VulnClassInclude) > 0 && !containsAnyFold(s.VulnClass, f.VulnClassInclude) {
		return false
	}
	return true
}

func intersectsInt(a, b []int64) bool {
	// A linear scan is fine for small sets; both sides are a few dozen hand-picked IDs,
	// and building a map costs more than it saves.
	for _, v := range b {
		if slices.Contains(a, v) {
			return true
		}
	}
	return false
}

// containsAnyFold reports whether s contains any of keywords (case-insensitive).
func containsAnyFold(s string, keywords []string) bool {
	lower := strings.ToLower(s)
	for _, kw := range keywords {
		kw = strings.ToLower(strings.TrimSpace(kw))
		if kw != "" && strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}
