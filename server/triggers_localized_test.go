package server

import "testing"

// Regression tests that keep triggers.go's trigger-config validation error messages in
// English. The English check reuses assertEnglishError (no Hangul, Han, or kana).

// TestTriggerErrorConstantsLocalized asserts that all 3 error constants are English.
func TestTriggerErrorConstantsLocalized(t *testing.T) {
	cases := []struct {
		name string
		msg  string
	}{
		{"no_condition", errTriggerNoCondition},
		{"tool_set_empty", errTriggerToolSetEmpty},
		{"custom_only", errTriggerCustomOnly},
	}
	for _, c := range cases {
		assertEnglishError(t, c.name, c.msg)
	}
}

// TestValidateTriggerLocalized really calls the pure validateTrigger function and
// confirms the missing-condition and no-tool-selected paths return the English
// constants while valid input returns an empty string. pgCreateTrigger and
// pgUpdateTrigger expose this return value to the user verbatim via writeErr(400, msg).
func TestValidateTriggerLocalized(t *testing.T) {
	if msg := validateTrigger(&triggerReq{}); msg != errTriggerNoCondition {
		t.Fatalf("missing-condition message = %q, want %q", msg, errTriggerNoCondition)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true}); msg != errTriggerToolSetEmpty {
		t.Fatalf("no-tool-selected message = %q, want %q", msg, errTriggerToolSetEmpty)
	}
	if msg := validateTrigger(&triggerReq{OnFinding: true}); msg != "" {
		t.Fatalf("valid-input message = %q, want empty string", msg)
	}
	if msg := validateTrigger(&triggerReq{OnToolCall: true, ToolNames: []string{"nmap"}}); msg != "" {
		t.Fatalf("valid tool-call message = %q, want empty string", msg)
	}
}
