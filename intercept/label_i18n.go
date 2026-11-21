package intercept

import "github.com/Autumn-27/artex/config"

// Translation table for intercept verdict action labels (allow/deny/ask). Used in
// verdict messages and follows the output language (config.ActiveLanguage).
// Unwired or unsupported languages fall back to en. The enum keys (allow/deny/ask)
// and the source marker ("[model]") are stable identifiers and are not translated.
var judgeActionLabels = map[string]map[string]string{
	"en": {"allow": "allow", "deny": "deny", "ask": "ask"},
	"ko": {"allow": "허용", "deny": "차단", "ask": "확인 요청"},
	"zh": {"allow": "允许", "deny": "拦截", "ask": "询问"},
	"es": {"allow": "permitir", "deny": "bloquear", "ask": "preguntar"},
	"ja": {"allow": "許可", "deny": "ブロック", "ask": "確認"},
}

func judgeActionLang() string {
	lang := config.ActiveLanguage()
	if _, ok := judgeActionLabels[lang]; ok {
		return lang
	}
	return "en"
}
