package server

import "github.com/Autumn-27/artex/config"

// Catalog that translates user-facing backend messages (English source) into the active
// output language. writeErr response text and some writeJSON error fields are written as
// English constants/literals; errCatalog (errmsg_catalog.go) keys them by that exact English
// text, with ko/zh/es/ja translations as values. English ("en") is the key itself and is
// not stored; on a lookup miss or when the language is "en", the source is returned as-is.
//
// Keys must match the source constants/literals byte-for-byte, otherwise the runtime value
// passed to writeErr silently misses its translation.
//
// For format-string constants (%d, %s, %w, ...) the formatted result no longer equals the
// key, so callers translate the template itself with trMsg(template) before passing it to
// fmt.Sprintf / fmt.Errorf.

// trMsg translates an English source message into the active output language. When it is
// not in the catalog or the active language is English, the source is returned unchanged.
// config.ActiveLanguage reflects the runtime setting (settings table), so the user's choice
// applies immediately.
func trMsg(src string) string {
	if m, ok := errCatalog[src]; ok {
		if v, ok2 := m[config.ActiveLanguage()]; ok2 && v != "" {
			return v
		}
	}
	return src
}

// loadOutputLanguage resolves the user-facing output language from the source of
// truth: the runtime setting (settings table) over config.Language() (ARTEX_LANG >
// config file > default). Call it only to (re)fill the cache; it does a DB query and a
// config file read.
func (s *Server) loadOutputLanguage() string {
	if s.m != nil && s.m.pg != nil {
		if v, ok, _ := s.m.pg.GetSetting(settingOutputLanguage); ok {
			if nv := config.NormalizeLanguage(v); nv != "" {
				return nv
			}
		}
	}
	return config.Language()
}

// outputLanguage returns the cached output language, resolving it on first use.
func (s *Server) outputLanguage() string {
	if v, ok := s.outputLang.Load().(string); ok && v != "" {
		return v
	}
	v := s.loadOutputLanguage()
	s.outputLang.Store(v)
	return v
}
