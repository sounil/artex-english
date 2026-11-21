package server

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"artex/db"
)

// Regression guard that keeps the notification-settings API responses and test
// message in notify_api.go in English. It reuses the F3a assertEnglishError helper
// (no Hangul, Han, or kana).

// TestNotifyErrorConstantsLocalized asserts that all 10 response constants and the 3
// test-message constants are English. Reverting any of them to Korean or Chinese
// fails this test.
func TestNotifyErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"notifyErrBadJSON":         notifyErrBadJSON,
		"notifyErrKindInvalidFmt":  fmt.Sprintf(notifyErrKindInvalidFmt, "email / webhook"),
		"notifyErrNameMissing":     notifyErrNameMissing,
		"notifyErrNameEmpty":       notifyErrNameEmpty,
		"notifyErrModeInvalid":     notifyErrModeInvalid,
		"notifyErrRateNegative":    notifyErrRateNegative,
		"notifyErrChannelID":       notifyErrChannelID,
		"notifyErrKindUnregFmt":    fmt.Sprintf(notifyErrKindUnregFmt, "bogus"),
		"notifyErrDeliveryID":      notifyErrDeliveryID,
		"notifyErrChannelNotFound": notifyErrChannelNotFound,
		"notifyTestName":           notifyTestName,
		"notifyTestClass":          notifyTestClass,
		"notifyTestSummary":        notifyTestSummary,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestNotifyChannelLookupErrLocalized exercises the real "channel not found → 404"
// path. notifyChannelLookupErr is a package function that needs only w and err (no
// Server/DB), so passing the sentinel error verifies, without a database, that the
// English constant ends up in the 404 response body.
func TestNotifyChannelLookupErrLocalized(t *testing.T) {
	rec := httptest.NewRecorder()
	notifyChannelLookupErr(rec, db.ErrNotificationChannelNotFound)
	if rec.Code != 404 {
		t.Fatalf("status code = %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
	got := decodeErrorField(t, rec.Body.Bytes())
	if got != notifyErrChannelNotFound {
		t.Fatalf("response message = %q, want %q", got, notifyErrChannelNotFound)
	}
	assertEnglishError(t, "channel_not_found", got)
}

// TestNotifyTestMessageLocalized builds the channel connectivity test message with
// the pure function and checks that the body sent to the user (name, class,
// summary) is English. No DB or network is needed.
func TestNotifyTestMessageLocalized(t *testing.T) {
	const base = "https://artex.example.test"
	msg := notifyTestMessage(base)
	if len(msg.Items) != 1 {
		t.Fatalf("test message item count = %d, want 1", len(msg.Items))
	}
	item := msg.Items[0]
	assertEnglishError(t, "test.name", item.Name)
	assertEnglishError(t, "test.class", item.VulnClass)
	assertEnglishError(t, "test.summary", item.Summary)
	if msg.HomeURL != base || item.DetailURL != base {
		t.Fatalf("links not preserved: HomeURL=%q DetailURL=%q (want %q)", msg.HomeURL, item.DetailURL, base)
	}
}
