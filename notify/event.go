package notify

// Snapshot is the contract for the notification_events.snapshot JSONB column. The
// writer is the db layer's finding-insert transaction; the readers are the server
// layer's delivery engine and filter matching. It is defined here because it is a
// notification-domain payload: db only serializes it and doesn't interpret fields.
//
// Why finding fields are copied instead of looked up at render time: findings get
// renamed, re-rated and re-triaged later, but a push should reflect the conclusion
// **at the time of the event**; a lookup could dangerously show "changed to low
// afterwards". It also spares fan-out and rendering a JOIN across findings/tasks/assets.
type Snapshot struct {
	// Event kind: finding_created / finding_status_changed
	Kind      string  `json:"kind"`
	FindingID int64   `json:"finding_id"`
	TaskID    int64   `json:"task_id"`
	VulnClass string  `json:"vulnclass"`
	Name      string  `json:"name"`
	Severity  string  `json:"severity"`
	Summary   string  `json:"summary"`
	AssetIDs  []int64 `json:"asset_ids"`
	// Non-empty only when kind=finding_status_changed.
	FromStatus string `json:"from_status,omitempty"`
	ToStatus   string `json:"to_status,omitempty"`
}

// Item is one finding to push, for channel rendering.
type Item struct {
	FindingID int64
	Name      string
	VulnClass string
	Severity  string
	Summary   string
	// Assets are resolved asset display names (e.g. domains/IPs), filled in by the
	// server layer: this package never touches the database and can't get names.
	Assets []string
	// DetailURL links back to the finding; empty means public_base_url isn't configured and it is omitted.
	DetailURL string
	// Status-change events only; when both are set it renders as "Pending → Fixed".
	FromStatus string
	ToStatus   string
}

// IsStatusChange reports whether the item is a status-change event.
func (i Item) IsStatusChange() bool { return i.FromStatus != "" || i.ToStatus != "" }

// Title returns the item's display title: the human-given name first, falling back to
// vulnclass, and a placeholder when both are empty, so the title is never blank.
func (i Item) Title() string {
	if i.Name != "" {
		return i.Name
	}
	if i.VulnClass != "" {
		return i.VulnClass
	}
	return "(untitled finding)"
}

// Message is the full content of one channel send.
type Message struct {
	// Length 1 for a single push; a whole batch for a digest push.
	// An empty slice is invalid; callers must supply at least one item.
	Items []Item
	// Batch=true renders as a digest (different title, with time window and count).
	Batch bool
	// WindowMinutes is the digest period in minutes, used for "last N minutes" text only
	// when Batch=true. Passed in explicitly from config rather than computed with
	// time.Since at render time, so rendering stays deterministic and testable.
	WindowMinutes int
	// HomeURL is the platform dashboard address (global public_base_url); empty omits the dashboard link.
	HomeURL string
}
