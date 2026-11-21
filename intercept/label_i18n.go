package intercept

// Display labels for intercept verdict actions (allow/deny/ask), used in verdict messages.
// The enum keys and the source marker ("[model]") are stable identifiers.
var judgeActionLabels = map[string]string{"allow": "allow", "deny": "deny", "ask": "ask"}
