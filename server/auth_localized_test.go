package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep the auth response messages in English (backlog F3b). They
// check two paths that run without a DB: (1) the requireAuth middleware does not go
// through pg, so the real HTTP response body is checked; (2) the remaining handler
// messages are named constants, so the constants themselves are checked. The English
// check reuses the assertEnglishError helper (no Hangul, Han, or kana).

// TestRequireAuthMessagesLocalized confirms through the real HTTP handler that the 401
// response body requireAuth returns for a missing or invalid token is English. This
// middleware does not use the database, so it runs end to end without a DB.
func TestRequireAuthMessagesLocalized(t *testing.T) {
	s := &Server{jwtKey: []byte(strings.Repeat("k", 32))}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := s.requireAuth(next)

	cases := []struct {
		name   string
		bearer string
		want   string
	}{
		{"no-token", "", authErrUnauthorized},
		{"invalid-token", "Bearer not-a-valid-token", authErrTokenInvalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/tasks", nil)
			if c.bearer != "" {
				r.Header.Set("Authorization", c.bearer)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, r)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status code = %d, want 401", rec.Code)
			}
			body := rec.Body.String()
			if !strings.Contains(body, c.want) {
				t.Errorf("response body is missing %q: %s", c.want, body)
			}
			assertEnglishError(t, c.name, body)
		})
	}
}

// TestAuthErrorConstantsLocalized asserts that every user-facing message constant used
// by the auth handlers is English (no Hangul, Han, or kana). It catches regressions,
// without a DB, even in the messages of handlers that requireAuth cannot reach
// directly (= that go through pg).
func TestAuthErrorConstantsLocalized(t *testing.T) {
	consts := map[string]string{
		"authErrUnauthorized":          authErrUnauthorized,
		"authErrTokenInvalid":          authErrTokenInvalid,
		"authErrPasswordAlreadySet":    authErrPasswordAlreadySet,
		"authErrPasswordEmpty":         authErrPasswordEmpty,
		"authErrNewPasswordEmpty":      authErrNewPasswordEmpty,
		"authErrPasswordHash":          authErrPasswordHash,
		"authErrSaveFailedPrefix":      authErrSaveFailedPrefix,
		"authErrTokenGen":              authErrTokenGen,
		"authErrBadRequest":            authErrBadRequest,
		"authErrPasswordNotInit":       authErrPasswordNotInit,
		"authErrCurrentPasswordWrong":  authErrCurrentPasswordWrong,
		"authErrBadCredential":         authErrBadCredential,
		"authErrPasswordTooShort":      authErrPasswordTooShort,
		"authErrSetupTokenInvalid":     authErrSetupTokenInvalid,
		"authErrTooManyAttempts":       authErrTooManyAttempts,
		"authErrPasswordTooLong":       authErrPasswordTooLong,
		"authErrDataSourceUnavailable": authErrDataSourceUnavailable,
	}
	for name, msg := range consts {
		assertEnglishError(t, name, msg)
	}
}
