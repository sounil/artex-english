package notify

import (
	"errors"
	"strings"
	"testing"
)

// Pins that the user-facing text of notify/mask.go and filter.go (validation errors
// that server/notify_api.go returns as is via writeErr(400) when notification channel
// settings are saved or updated) is English. channel.go has no user-facing text (only
// comments) and is not covered. hasHan / hasHangul / assertEnglish are defined in
// notify_localized_test.go.

// TestFilterValidateLocalized checks the validation error for a bad min_severity.
// It is surfaced as a 400 on the save/update path (server/notify_api.go).
func TestFilterValidateLocalized(t *testing.T) {
	err := Filter{MinSeverity: "hgih"}.Validate()
	if err == nil {
		t.Fatal("invalid minimum severity but no error")
	}
	assertEnglish(t, "Filter.Validate", err.Error())
	// low/medium/high/critical are config enums and must appear verbatim.
	for _, tok := range []string{"low", "medium", "high", "critical"} {
		if !strings.Contains(err.Error(), tok) {
			t.Errorf("severity token %q is missing: %q", tok, err.Error())
		}
	}
	// Valid and empty values must pass.
	if err := (Filter{MinSeverity: "high"}).Validate(); err != nil {
		t.Errorf("high is valid but got an error: %v", err)
	}
	if err := (Filter{}).Validate(); err != nil {
		t.Errorf("empty minimum severity is valid but got an error: %v", err)
	}
}

// TestPrepareConfigUpdateUnknownKindLocalized checks the error when updating config
// with an unregistered channel kind (a 400 in server/notify_api.go).
func TestPrepareConfigUpdateUnknownKindLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("definitely-not-a-channel", map[string]any{}, map[string]any{})
	if err == nil {
		t.Fatal("unregistered channel kind but no error")
	}
	assertEnglish(t, "PrepareConfigUpdate unknown kind", err.Error())
}

// TestDestinationChangedErrorLocalized triggers ErrDestinationChangedWithoutCredentials
// via the real path: a PATCH that changes only the webhook destination (url) and says
// nothing about the credentials (headers). The server returns this as a 400.
func TestDestinationChangedErrorLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{
			"url":     "https://old.example.com/hook",
			"headers": map[string]any{"Authorization": "Bearer real-token"},
		},
		map[string]any{"url": "https://attacker.example.com/hook"},
	)
	if err == nil {
		t.Fatal("destination changed without restating credentials, but no error")
	}
	var de *ErrDestinationChangedWithoutCredentials
	if !errors.As(err, &de) {
		t.Fatalf("unexpected error type: %T", err)
	}
	assertEnglish(t, "ErrDestinationChangedWithoutCredentials", err.Error())
	// The changed destination key and missing credential key are config field names and
	// must appear verbatim in the message.
	if !strings.Contains(err.Error(), "url") || !strings.Contains(err.Error(), "headers") {
		t.Errorf("field key names are missing: %q", err.Error())
	}
}

// TestRejectMaskedInContainersLocalized triggers rejection of a mask sentinel embedded
// in a structure via the real path: a PATCH that puts the sentinel inside headers (an
// object that is also a credential field).
func TestRejectMaskedInContainersLocalized(t *testing.T) {
	_, err := PrepareConfigUpdate("webhook",
		map[string]any{},
		map[string]any{"headers": map[string]any{"Authorization": MaskedPrefix + ":…abc123"}},
	)
	if err == nil {
		t.Fatal("mask sentinel embedded in a structure but no error")
	}
	assertEnglish(t, "rejectMaskedInContainers", err.Error())
	// The sentinel literal (__masked__) is shown so operators can tell which value is wrong.
	if !strings.Contains(err.Error(), MaskedPrefix) {
		t.Errorf("mask sentinel missing from the message: %q", err.Error())
	}
}
