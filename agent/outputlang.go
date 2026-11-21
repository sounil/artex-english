package agent

// User-facing output language selection. artex-global forces the natural language the agent
// shows to the user to be one of five languages: English, Korean, Chinese, Spanish or Japanese.
// The agent's reasoning body (segment [A]) is language-independent and left as is. What is
// decided here is only the language of the surface the user reads.

// OutputLanguage is a hook the server wires to return the output language code
// ("en"/"ko"/"zh"/"es"/"ja") chosen from settings (ARTEX_LANG in config, the runtime settings KV).
// When nil or empty, the default below applies. The server injects it the same way as PromptOverride.
var OutputLanguage func() string

// Default output language when the server has not wired the hook (e.g. agent package tests
// run alone). In production the server wires the OutputLanguage hook to the settings-layer
// default, which is also English. Other languages are switched via settings.
const defaultOutputLang = "en"

// SupportedOutputLangs lists the output language codes that can be selected.
var SupportedOutputLangs = []string{"en", "ko", "zh", "es", "ja"}

// normalizeLang narrows codes outside the supported list to the default.
func normalizeLang(code string) string {
	for _, c := range SupportedOutputLangs {
		if c == code {
			return code
		}
	}
	return defaultOutputLang
}

// resolveOutputLang returns the output language code to apply now. It checks the
// server-wired hook first and falls back to the default.
func resolveOutputLang() string {
	if OutputLanguage != nil {
		if code := OutputLanguage(); code != "" {
			return normalizeLang(code)
		}
	}
	return defaultOutputLang
}

// langPhrase is the label used when a directive names "write in this language".
// The autonym in parentheses keeps the model from confusing which language is meant.
var langPhrase = map[string]string{
	"ko": "Korean (한국어)",
	"en": "English",
	"zh": "Simplified Chinese (简体中文)",
	"es": "Spanish (Español)",
	"ja": "Japanese (日本語)",
}

// langBare is the short language name used when a directive refers to it repeatedly.
var langBare = map[string]string{
	"ko": "Korean",
	"en": "English",
	"zh": "Chinese",
	"es": "Spanish",
	"ja": "Japanese",
}
