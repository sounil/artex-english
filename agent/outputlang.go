package agent

// User-facing output language. ARTEX writes everything the user reads in English; langDirective
// (prompt.go) tells each agent so, even when the target, its evidence or a stored prompt is in
// another language. The agent's internal reasoning is not restricted.

// outputLang is the language of user-visible agent text.
const outputLang = "en"

// resolveOutputLang returns the output language code (always English).
func resolveOutputLang() string { return outputLang }

// langPhrase / langBare name the output language inside the directive.
var (
	langPhrase = map[string]string{"en": "English"}
	langBare   = map[string]string{"en": "English"}
)
