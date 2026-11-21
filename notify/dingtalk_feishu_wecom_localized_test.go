package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// asciiStatusChangeItem builds an ASCII-only status-change item. Only when the data
// itself has no CJK can an "entire output is English" assertion catch a label
// regression precisely.
func asciiStatusChangeItem() Item {
	return Item{
		FindingID:  7,
		Name:       "login-flaw",
		VulnClass:  "SQLi",
		Severity:   "high",
		Summary:    "SQL injection via q param",
		Assets:     []string{"a.example.com"},
		DetailURL:  "https://platform.example/finding/7",
		FromStatus: "pending",
		ToStatus:   "fixed",
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("card marshal failed: %v", err)
	}
	return string(b)
}

// TestFeishuItemLinesLocalized checks the Feishu single-card field labels (status
// change, type, assets, summary) are English and identical to markdown.go's. feishu.go
// has its own copy of these labels (not the shared markdown function), so this pin is
// needed separately to catch a regression in the Feishu body alone.
func TestFeishuItemLinesLocalized(t *testing.T) {
	got := feishuItemLines(asciiStatusChangeItem())
	assertEnglish(t, "feishuItemLines", got)
	for _, want := range []string{
		"**Status change**: Pending → Fixed",
		"**Type**: SQLi",
		"**Assets**: a.example.com",
		"**Summary**: SQL injection via q param",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Feishu single body should contain %q:\n%s", want, got)
		}
	}
}

// TestFeishuCardButtonsLocalized checks the Feishu card button labels (View details,
// View all on the platform) are English and the whole card is free of CJK (with
// ASCII data).
func TestFeishuCardButtonsLocalized(t *testing.T) {
	// Single: "View details" button (when DetailURL is set)
	single, _ := feishuCard(Message{Items: []Item{asciiStatusChangeItem()}})
	singleJSON := mustJSON(t, single)
	assertEnglish(t, "single Feishu card", singleJSON)
	if !strings.Contains(singleJSON, "View details") {
		t.Errorf("single Feishu card should have a 'View details' button:\n%s", singleJSON)
	}

	// Batch: "View all on the platform" button (when HomeURL is set)
	batch, _ := feishuCard(Message{Batch: true, HomeURL: "https://platform.example", Items: hanFreeItems(2)})
	batchJSON := mustJSON(t, batch)
	assertEnglish(t, "batch Feishu card", batchJSON)
	if !strings.Contains(batchJSON, "View all on the platform") {
		t.Errorf("batch Feishu card should have a 'View all on the platform' button:\n%s", batchJSON)
	}
}

// TestDingTalkActionCardButtonLocalized checks the DingTalk ActionCard "View details"
// button (singleTitle) is English via the real Send path (fake receiver).
func TestDingTalkActionCardButtonLocalized(t *testing.T) {
	var singleTitle string
	srv := capturePost(t, `{"errcode":0,"errmsg":"ok"}`, func(_ *testing.T, body map[string]any, _ *http.Request) {
		card, _ := body["actionCard"].(map[string]any)
		singleTitle, _ = card["singleTitle"].(string)
	})
	m := Message{Items: []Item{asciiStatusChangeItem()}}
	if _, err := (dingTalkChannel{}).Send(context.Background(), map[string]any{"webhook": srv.URL}, m); err != nil {
		t.Fatalf("delivery failed: %v", err)
	}
	if singleTitle != "View details" {
		t.Errorf("DingTalk ActionCard singleTitle should be 'View details', got %q", singleTitle)
	}
}

// TestChinaPlatformValidateLocalized checks DingTalk / Feishu / WeCom config validation
// errors are English and mention the webhook (shown to users by the notification
// settings save / test APIs).
func TestChinaPlatformValidateLocalized(t *testing.T) {
	for _, kind := range []string{KindDingTalk, KindFeishu, KindWeCom} {
		ch, ok := Get(kind)
		if !ok {
			t.Fatalf("channel %s is not registered", kind)
		}

		// Empty config: "webhook URL is missing"
		missing := ch.Validate(map[string]any{})
		if missing == nil {
			t.Fatalf("%s: empty config should fail validation", kind)
		}
		assertEnglish(t, kind+" missing", missing.Error())
		if missing.Error() != "webhook URL is missing" {
			t.Errorf("%s: missing error should be 'webhook URL is missing', got %q", kind, missing)
		}

		// Bad URL (ftp): "invalid webhook URL: …"
		bad := ch.Validate(map[string]any{"webhook": "ftp://x"})
		if bad == nil {
			t.Fatalf("%s: an ftp URL should fail validation", kind)
		}
		assertEnglish(t, kind+" bad url", bad.Error())
		if !strings.HasPrefix(bad.Error(), "invalid webhook URL") {
			t.Errorf("%s: bad URL error should start with 'invalid webhook URL', got %q", kind, bad)
		}
	}
}

// TestChinaPlatformSendErrorsLocalized checks DingTalk / Feishu / WeCom send failure
// reasons land in the delivery history (last_error) in English. Platform names match
// the UI channel labels (DingTalk/Feishu/WeCom). The fake server errmsg is ASCII, so
// any CJK in the error means the message skeleton regressed.
func TestChinaPlatformSendErrorsLocalized(t *testing.T) {
	// Business error code path (HTTP 200 + errcode/code != 0 in the body).
	bizCases := []struct {
		kind     string
		resp     string
		wantSubs []string
	}{
		{KindDingTalk, `{"errcode":310000,"errmsg":"keyword not matched"}`, []string{"DingTalk returned an error", "310000"}},
		{KindFeishu, `{"code":19021,"msg":"sign error"}`, []string{"Feishu returned an error", "19021"}},
		{KindWeCom, `{"errcode":45009,"errmsg":"freq out of limit"}`, []string{"WeCom rate limited the request", "45009"}},
		{KindWeCom, `{"errcode":93000,"errmsg":"invalid webhook"}`, []string{"WeCom returned an error", "93000"}},
	}
	for _, tc := range bizCases {
		srv := capturePost(t, tc.resp, nil)
		ch, ok := Get(tc.kind)
		if !ok {
			t.Fatalf("channel %s is not registered", tc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: response %s should be an error", tc.kind, tc.resp)
		}
		assertEnglish(t, tc.kind+" send error", err.Error())
		for _, sub := range tc.wantSubs {
			if !strings.Contains(err.Error(), sub) {
				t.Errorf("%s: send error should contain %q, got %q", tc.kind, sub, err)
			}
		}
	}

	// Parse failure path when the response is not JSON: "could not parse <platform> response".
	parseCases := []struct {
		kind     string
		platform string
	}{
		{KindDingTalk, "DingTalk"},
		{KindFeishu, "Feishu"},
		{KindWeCom, "WeCom"},
	}
	for _, pc := range parseCases {
		srv := capturePost(t, `not-json`, nil)
		ch, ok := Get(pc.kind)
		if !ok {
			t.Fatalf("channel %s is not registered", pc.kind)
		}
		_, err := ch.Send(context.Background(), map[string]any{"webhook": srv.URL}, singleMsg())
		if err == nil {
			t.Fatalf("%s: a non-JSON response should be an error", pc.kind)
		}
		want := "could not parse " + pc.platform + " response"
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%s: parse failure error should contain %q, got %q", pc.kind, want, err)
		}
	}
}
