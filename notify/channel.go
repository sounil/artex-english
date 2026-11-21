package notify

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
)

// Channel is a notification channel adapter. Implementations must be **stateless**:
// one instance is reused concurrently by many channel configs, and all credentials
// come in through the cfg argument.
type Channel interface {
	// Kind returns the channel kind identifier, matching its registry key.
	Kind() string
	// Validate is called when a config is saved and checks required fields and formats.
	// The returned error is shown directly to whoever is configuring it, so it should
	// say which field is missing rather than a generic "invalid config".
	Validate(cfg map[string]any) error
	// Send delivers one message and returns the **number of items actually delivered**
	// and an error.
	//
	// Why return a count: every platform has a message length limit, and a digest that
	// can't fit the whole batch gets cut. If the caller unconditionally marked the whole
	// batch delivered, the cut items would vanish: not in the message, shown as success
	// in the delivery history, with nowhere to discover they were never sent. With kept
	// returned, the caller marks only the first kept items and leaves the rest for the
	// next batch.
	//
	// An error means delivery failed; a *PermanentError means don't retry.
	// On failure kept is meaningless and callers should ignore it.
	Send(ctx context.Context, cfg map[string]any, m Message) (int, error)
	// DefaultRatePerMin returns the channel's officially recommended per-minute delivery
	// limit, used as the default rate limit for new channel instances. 0 means no known limit.
	DefaultRatePerMin() int
	// SecretKeys returns the config keys that hold credentials. Their values are masked
	// when echoed by the API, and a masked value received on update keeps the stored
	// value. Only the implementation knows which fields are credentials (for WeCom the
	// whole webhook URL is the credential, for DingTalk only its secret), so this must
	// come from the channel, not be guessed by upper layers.
	SecretKeys() []string
	// DestinationKeys returns the config keys that decide where messages are sent.
	//
	// Security-relevant like SecretKeys: destination and credentials are separate
	// fields, and if "change only the destination, keep the credentials" were allowed,
	// anyone able to edit a channel config could send the real stored credentials to a
	// server they control, making config masking pointless.
	// See PrepareConfigUpdate.
	DestinationKeys() []string
}

// registry is the channel registry. Deliberately an explicit literal rather than init()
// self-registration: all channels are visible in one place, and a new channel exposes
// omissions at compile time instead of relying on runtime side effects.
var registry = map[string]Channel{
	KindDingTalk: dingTalkChannel{},
	KindFeishu:   feishuChannel{},
	KindWeCom:    weComChannel{},
	KindWebhook:  webhookChannel{},
	KindTelegram: telegramChannel{},
	KindEmail:    emailChannel{},
}

// Get returns the channel implementation for a kind.
func Get(kind string) (Channel, bool) {
	c, ok := registry[kind]
	return c, ok
}

// ValidKind reports whether kind is a supported channel kind.
func ValidKind(kind string) bool {
	_, ok := registry[kind]
	return ok
}

// Kinds returns all supported channel kinds in lexical order (stable for UI dropdowns).
func Kinds() []string {
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// PermanentError marks a delivery failure that should not be retried: bad
// credentials, target rejection, invalid request body, etc. Retrying only helps
// transient faults (network blips, rate limits, peer 5xx); retrying a permanent
// failure never succeeds and buries the real error in retry logs.
type PermanentError struct{ Err error }

func (e *PermanentError) Error() string { return e.Err.Error() }
func (e *PermanentError) Unwrap() error { return e.Err }

// Permanent marks err as a permanent failure. Returns nil for a nil err, so you can
// write `return Permanent(someCheck())`.
func Permanent(err error) error {
	if err == nil {
		return nil
	}
	return &PermanentError{Err: err}
}

// IsPermanent reports whether err's chain carries a permanent-failure mark.
func IsPermanent(err error) bool {
	var pe *PermanentError
	return errors.As(err, &pe)
}

// ---- Config reading helpers ----
//
// Channel config comes from a database JSONB column; after encoding/json it is a
// map[string]any where numbers are float64 and arrays are []any. These helpers unify
// that conversion and tolerate type drift from the UI (e.g. a port entered as a string).

// cfgString reads a string config value, trimming surrounding whitespace (easily
// picked up when copy-pasting into a web form).
func cfgString(cfg map[string]any, key string) string {
	v, ok := cfg[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// cfgInt reads an integer config value, accepting float64 (JSON default) or a string.
func cfgInt(cfg map[string]any, key string) int {
	switch v := cfg[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// cfgBool reads a boolean config value, also accepting the strings "true"/"1".
func cfgBool(cfg map[string]any, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	default:
		return false
	}
}

// cfgStrings reads a string-array config value, trimming whitespace and dropping empty strings.
func cfgStrings(cfg map[string]any, key string) []string {
	raw, ok := cfg[key].([]any)
	if !ok {
		// Also accept a single string, for form submissions with just one value.
		if s := cfgString(cfg, key); s != "" {
			return []string{s}
		}
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			continue
		}
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// cfgMap reads a string-map config value (e.g. custom HTTP headers), trimming keys and
// dropping empty keys.
func cfgMap(cfg map[string]any, key string) map[string]string {
	raw, ok := cfg[key].(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]string, len(raw))
	for k, v := range raw {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		s, ok := v.(string)
		if !ok {
			continue
		}
		out[k] = s
	}
	return out
}
