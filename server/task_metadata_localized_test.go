package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Regression tests that keep the error responses of task_metadata.go's task metadata
// update API in English. The English check reuses assertEnglishError (no Hangul, Han,
// or kana).

// TestTaskMetadataErrorConstantsLocalized asserts that all 4 response constants are
// English. Reverting any one to Korean or Chinese fails this test.
func TestTaskMetadataErrorConstantsLocalized(t *testing.T) {
	cases := map[string]string{
		"errTaskMetaRequestTooLarge": errTaskMetaRequestTooLarge,
		"errTaskMetaNoFields":        errTaskMetaNoFields,
		"errTaskMetaNameEmpty":       errTaskMetaNameEmpty,
		"errTaskMetaNameTooLongFmt":  fmt.Sprintf(errTaskMetaNameTooLongFmt, maxTaskNameRunes),
	}
	for label, msg := range cases {
		assertEnglishError(t, label, msg)
	}
}

// TestUpdateTaskMetadataResponsesLocalized checks the 4 input-validation paths all the
// way to the real HTTP response body. These 4 paths of updateTaskMetadata (body too
// large, no fields, blank name, name too long) look only at s.m.Task (a map lookup)
// and the request body and return before s.m.UpdateTaskMetadata (DB), so a single
// task in the tasks map lets them run end to end without a DB. It also confirms the
// constants actually reach the response.
func TestUpdateTaskMetadataResponsesLocalized(t *testing.T) {
	s := &Server{m: &Manager{tasks: map[string]*Task{"t1": {ID: "t1"}}}}

	call := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPatch, "/api/tasks/t1/metadata", strings.NewReader(body))
		req.SetPathValue("id", "t1")
		rec := httptest.NewRecorder()
		s.updateTaskMetadata(rec, req)
		return rec
	}
	errBody := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		var resp struct {
			Error string `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("failed to parse response JSON: %v (body %q)", err, rec.Body.String())
		}
		return resp.Error
	}

	cases := []struct {
		name string
		body string
		code int
		want string
	}{
		// Valid JSON body over the 16KB limit. MaxBytesReader reports the overflow mid-read.
		{"request_too_large", `{"name":"` + strings.Repeat("a", maxTaskMetadataRequestSize+1024) + `"}`, http.StatusRequestEntityTooLarge, errTaskMetaRequestTooLarge},
		// Omitting both name and pinned leaves no field to update.
		{"no_fields", `{}`, http.StatusBadRequest, errTaskMetaNoFields},
		{"name_empty", `{"name":"  "}`, http.StatusBadRequest, errTaskMetaNameEmpty},
		{"name_too_long", `{"name":"` + strings.Repeat("가", maxTaskNameRunes+1) + `"}`, http.StatusBadRequest, fmt.Sprintf(errTaskMetaNameTooLongFmt, maxTaskNameRunes)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := call(c.body)
			if rec.Code != c.code {
				t.Fatalf("status code = %d, want %d (body %q)", rec.Code, c.code, rec.Body.String())
			}
			if got := errBody(t, rec); got != c.want {
				t.Fatalf("response message = %q, want %q", got, c.want)
			}
			assertEnglishError(t, c.name, errBody(t, rec))
		})
	}
}
