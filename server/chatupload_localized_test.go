package server

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Regression tests that keep the error responses of chatupload.go's chat attachment
// upload API in English. The English check reuses assertEnglishError (no Hangul, Han,
// or kana) and response-body extraction reuses decodeErrorField from the
// task_categories tests (same package server).

// TestChatUploadErrorConstantsLocalized asserts that all 7 response constants are
// English. The three "... failed" ones are prefix constants followed by err.Error(),
// so they end in ": ", but they still pass as long as they are English.
func TestChatUploadErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"scope_invalid": errChatUploadScopeInvalid,
		"bad_id":        errChatUploadBadID,
		"task_deleting": errChatUploadTaskDeleting,
		"mkdir":         errChatUploadMkdir,
		"parse":         errChatUploadParse,
		"no_file":       errChatUploadNoFile,
		"save_failed":   errChatUploadSaveFailed,
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestChatUploadHandlerResponsesLocalized checks chatUpload paths that finish without
// touching the DB all the way to the real HTTP response body. scope/id validation
// returns before touching the Manager; no-file uses scope=session (not a task), so it
// finishes with only a temp directory and no engine/DB. task-deleting yields 409 via
// the same delete barrier (engine.deleting) as the goals test.
func TestChatUploadHandlerResponsesLocalized(t *testing.T) {
	// Multipart body with only a field and no "file" — ParseMultipartForm passes but there are 0 files.
	noFileBody := func() (*bytes.Buffer, string) {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		_ = mw.WriteField("other", "x")
		_ = mw.Close()
		return &buf, mw.FormDataContentType()
	}

	t.Run("scope-invalid", func(t *testing.T) {
		s := &Server{}
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=bogus&id=x", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadScopeInvalid {
			t.Fatalf("response message = %q, want %q", got, errChatUploadScopeInvalid)
		}
		assertEnglishError(t, "scope-invalid", errChatUploadScopeInvalid)
	})

	t.Run("bad-id", func(t *testing.T) {
		s := &Server{}
		// scope=session (not a task), so it returns at id validation before touching the Manager.
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=session&id=../evil", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadBadID {
			t.Fatalf("response message = %q, want %q", got, errChatUploadBadID)
		}
		assertEnglishError(t, "bad-id", errChatUploadBadID)
	})

	t.Run("no-file", func(t *testing.T) {
		s := &Server{m: &Manager{dir: t.TempDir()}}
		body, ctype := noFileBody()
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=session&id=sess1", body)
		req.Header.Set("Content-Type", ctype)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status code = %d, want 400 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadNoFile {
			t.Fatalf("response message = %q, want %q", got, errChatUploadNoFile)
		}
		assertEnglishError(t, "no-file", errChatUploadNoFile)
	})

	t.Run("task-deleting", func(t *testing.T) {
		s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}, engine: &Engine{}}
		s.engine.deleting.Store("t1", true)
		req := httptest.NewRequest(http.MethodPost, "/api/chat/upload?scope=task&id=t1", nil)
		rec := httptest.NewRecorder()
		s.chatUpload(rec, req)
		if rec.Code != http.StatusConflict {
			t.Fatalf("status code = %d, want 409 (body %q)", rec.Code, rec.Body.String())
		}
		if got := decodeErrorField(t, rec.Body.Bytes()); got != errChatUploadTaskDeleting {
			t.Fatalf("response message = %q, want %q", got, errChatUploadTaskDeleting)
		}
		assertEnglishError(t, "task-deleting", errChatUploadTaskDeleting)
	})
}
