package server

import "testing"

// Regression test that keeps the user-facing error messages returned by the intercept
// rule validators in asset_intercept.go and task_intercept.go in English. Both
// validators (validateAssetInterceptRuleReq, validateTaskInterceptRuleReq) are pure
// functions that do not use the DB, so besides pinning the constants the real error
// paths are called directly and their returned messages verified. The English check
// reuses assertEnglishError (no Hangul, Han, or kana). Field names and enum values
// (pattern, exact_ip, cidr, kind, action, block, allow) are wire identifiers and stay
// ASCII. If anyone reverts the surrounding text to Korean or Chinese, this fails.
func TestAssetInterceptErrorsLocalized(t *testing.T) {
	// Constant pins
	for _, c := range []struct{ label, msg string }{
		{"pattern_empty", errAssetInterceptPatternEmpty},
		{"bad_exact_ip", errAssetInterceptInvalidExactIPFmt},
		{"bad_cidr", errAssetInterceptInvalidCIDRFmt},
		{"bad_kind", errAssetInterceptInvalidKindFmt},
		{"bad_action", errTaskInterceptInvalidAction},
	} {
		assertEnglishError(t, c.label, c.msg)
	}

	// Real paths: validateAssetInterceptRuleReq's four rejection branches
	for _, c := range []struct {
		label string
		req   assetInterceptRuleReq
	}{
		{"empty_pattern", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "   "}},
		{"invalid_exact_ip", assetInterceptRuleReq{Kind: "exact_ip", Pattern: "not-an-ip"}},
		{"invalid_cidr", assetInterceptRuleReq{Kind: "cidr", Pattern: "nonsense"}},
		{"invalid_kind", assetInterceptRuleReq{Kind: "nope", Pattern: "example.com"}},
	} {
		req := c.req
		err := validateAssetInterceptRuleReq(&req)
		if err == nil {
			t.Fatalf("%s: no error (validation passed)", c.label)
		}
		assertEnglishError(t, c.label, err.Error())
	}

	// Valid input is not rejected
	okReq := assetInterceptRuleReq{Kind: "cidr", Pattern: "192.168.0.0/16"}
	if err := validateAssetInterceptRuleReq(&okReq); err != nil {
		t.Fatalf("a valid CIDR was rejected: %v", err)
	}

	// Real path: validateTaskInterceptRuleReq's action rejection branch
	badAction := taskInterceptRuleReq{Action: "nope", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&badAction); err == nil {
		t.Fatalf("bad_action_path: no error (validation passed)")
	} else {
		assertEnglishError(t, "bad_action_path", err.Error())
	}

	// An empty action is normalized to the block default and passes
	defaultAction := taskInterceptRuleReq{Action: "", Kind: "exact_domain", Pattern: "example.com"}
	if err := validateTaskInterceptRuleReq(&defaultAction); err != nil {
		t.Fatalf("default action normalization failed: %v", err)
	}
	if defaultAction.Action != "block" {
		t.Fatalf("default action was not normalized to block: %q", defaultAction.Action)
	}
}
