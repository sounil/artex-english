package selfupdate

// Regression tests (F20) pinning that the user-facing progress and error text of the self-update
// path is English source text. updateHub in update.go forwards Stage's progress callback messages as
// SSE progress.message, and errors returned by Stage/FetchLatest/Rollback as progress.error, the
// writeErr body and boot_notice, straight to the frontend (update-card). Here we only assert
// "no Hangul, no Han characters, no kana".

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

// hasCJK reports whether s contains Hangul, Han characters or kana.
func hasCJK(s string) bool {
	for _, r := range s {
		switch {
		case r >= 0xAC00 && r <= 0xD7A3, // Hangul syllables
			r >= 0x3130 && r <= 0x318F, // Hangul compatibility jamo
			r >= 0x4E00 && r <= 0x9FFF, // CJK unified ideographs
			r >= 0x3040 && r <= 0x30FF: // hiragana / katakana
			return true
		}
	}
	return false
}

// assertEnglishError asserts the error exists and contains no CJK characters.
// English from wrapped (%w) stdlib errors is naturally allowed.
func assertEnglishError(t *testing.T, label string, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: expected an error, got nil", label)
	}
	msg := err.Error()
	if hasCJK(msg) {
		t.Errorf("%s: error contains CJK text: %q", label, msg)
	}
}

// rtFunc injects fake responses into http.Client. It never contacts GitHub.
type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func stubClient(status int, body string) *http.Client {
	return &http.Client{Transport: rtFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: status,
			Body:       io.NopCloser(strings.NewReader(body)),
			Header:     make(http.Header),
		}, nil
	})}
}

// TestCheckURLErrorsLocalized: TestCheckURLRejectsNonGitHub only checks rejection, not
// the message language. Here we check both rejection paths produce English errors.
func TestCheckURLErrorsLocalized(t *testing.T) {
	nonHTTPS := checkURL(mustParse(t, "http://api.github.com/x"))
	assertEnglishError(t, "non-https", nonHTTPS)
	if !strings.Contains(nonHTTPS.Error(), "HTTPS") {
		t.Errorf("non-https error does not mention HTTPS: %q", nonHTTPS.Error())
	}

	nonGitHub := checkURL(mustParse(t, "https://evil.example.com/x"))
	assertEnglishError(t, "non-github-host", nonGitHub)
	if !strings.Contains(nonGitHub.Error(), "GitHub") {
		t.Errorf("non-github error does not mention GitHub: %q", nonGitHub.Error())
	}
}

// TestFetchLatestErrorsLocalized: checks the version-check errors (rate limit, no release, other
// status, parse failure, missing tag) are English. A fake transport avoids the network.
func TestFetchLatestErrorsLocalized(t *testing.T) {
	ctx := context.Background()

	_, errRate := FetchLatest(ctx, stubClient(http.StatusForbidden, ""), DefaultRepo)
	assertEnglishError(t, "rate-limited", errRate)
	if !strings.Contains(errRate.Error(), "60") {
		t.Errorf("rate-limit error does not mention 60: %q", errRate.Error())
	}

	_, errNF := FetchLatest(ctx, stubClient(http.StatusNotFound, ""), DefaultRepo)
	assertEnglishError(t, "not-found", errNF)

	_, errStatus := FetchLatest(ctx, stubClient(http.StatusInternalServerError, ""), DefaultRepo)
	assertEnglishError(t, "bad-status", errStatus)
	if !strings.Contains(errStatus.Error(), "GitHub") {
		t.Errorf("status error does not mention GitHub: %q", errStatus.Error())
	}

	_, errJSON := FetchLatest(ctx, stubClient(http.StatusOK, "{not json"), DefaultRepo)
	assertEnglishError(t, "bad-json", errJSON)

	_, errTag := FetchLatest(ctx, stubClient(http.StatusOK, "{}"), DefaultRepo)
	assertEnglishError(t, "missing-tag", errTag)
	if !strings.Contains(errTag.Error(), "tag") {
		t.Errorf("missing-tag error does not mention tag: %q", errTag.Error())
	}
}

// TestStageMissingAssetLocalized: when the release lacks a package for this platform, Stage
// stops with an English error before downloading (FindAsset fails → no network access). Depending
// on the environment checkWritable may block first, but that error is English too, so only the language is asserted.
func TestStageMissingAssetLocalized(t *testing.T) {
	rel := &Release{TagName: "v9.9.9"} // no Assets
	err := Stage(context.Background(), &http.Client{}, rel, "0.0.1", nil)
	assertEnglishError(t, "stage-missing-asset", err)
}
