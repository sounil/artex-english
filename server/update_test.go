package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Autumn-27/artex/selfupdate"
)

// releaseCache is the layer that protects the GitHub quota: the unauthenticated API
// allows only 60 requests/hour/IP. Only an explicit "Check for updates" fetches; the
// top-bar badge reads the cache through peek and never reaches GitHub itself.

// testRepo is the source the cache tests query; the fake fetchers ignore it.
const testRepo = "owner/repo"

func newTestCache(fetch func(context.Context, *http.Client) (*selfupdate.Release, error)) *releaseCache {
	return &releaseCache{fetch: func(ctx context.Context, c *http.Client, _ string) (*selfupdate.Release, error) {
		return fetch(ctx, c)
	}}
}

func TestReleaseCacheServesFromCache(t *testing.T) {
	calls := 0
	c := newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return &selfupdate.Release{TagName: "v0.3.8"}, nil
	})

	for range 5 {
		rel, err := c.get(t.Context(), nil, testRepo, false)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if rel.TagName != "v0.3.8" {
			t.Fatalf("TagName = %q", rel.TagName)
		}
	}
	if calls != 1 {
		t.Errorf("5 lookups should hit the origin only once, got %d", calls)
	}
}

func TestReleaseCacheForceBypasses(t *testing.T) {
	calls := 0
	c := newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return &selfupdate.Release{TagName: "v0.3.8"}, nil
	})

	if _, err := c.get(t.Context(), nil, testRepo, false); err != nil {
		t.Fatal(err)
	}
	// Clicking "Check for updates" must get a live result; otherwise a just-published release stays invisible until the cache expires.
	if _, err := c.get(t.Context(), nil, testRepo, true); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("force should bypass the cache, want 2 origin hits, got %d", calls)
	}
}

func TestReleaseCacheExpiresAfterTTL(t *testing.T) {
	calls := 0
	c := newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return &selfupdate.Release{TagName: "v0.3.8"}, nil
	})

	if _, err := c.get(t.Context(), nil, testRepo, false); err != nil {
		t.Fatal(err)
	}
	// Move the stored timestamp back to just past expiry to simulate the TTL elapsing.
	c.at = time.Now().Add(-releaseTTL - time.Second)
	if _, err := c.get(t.Context(), nil, testRepo, false); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Errorf("should hit the origin again after the TTL expires, want 2, got %d", calls)
	}
}

func TestReleaseCacheUsesShorterTTLForErrors(t *testing.T) {
	calls := 0
	c := newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return nil, errors.New("github unreachable")
	})

	if _, err := c.get(t.Context(), nil, testRepo, false); err == nil {
		t.Fatal("expected an error")
	}
	// Failures are cached briefly too; otherwise every page load waits out a timeout while GitHub is unreachable.
	if _, err := c.get(t.Context(), nil, testRepo, false); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 1 {
		t.Errorf("errors should be cached briefly, want 1 origin hit, got %d", calls)
	}

	// But the error TTL must be clearly shorter than the success TTL so it self-heals quickly once the network recovers.
	if releaseErrTTL >= releaseTTL {
		t.Fatalf("error TTL (%v) must be shorter than success TTL (%v)", releaseErrTTL, releaseTTL)
	}
	c.at = time.Now().Add(-releaseErrTTL - time.Second)
	if _, err := c.get(t.Context(), nil, testRepo, false); err == nil {
		t.Fatal("expected an error")
	}
	if calls != 2 {
		t.Errorf("should retry after the error TTL expires, want 2, got %d", calls)
	}
}

func TestReleaseCacheDoesNotPoisonOnCallerCancel(t *testing.T) {
	good := &selfupdate.Release{TagName: "v0.3.8"}
	c := newTestCache(func(ctx context.Context, _ *http.Client) (*selfupdate.Release, error) {
		return good, nil
	})
	if _, err := c.get(t.Context(), nil, testRepo, false); err != nil {
		t.Fatal(err)
	}

	// A visitor closing the tab cancels the request. That says nothing about GitHub,
	// so "canceled" must never be cached — otherwise every visitor for the next
	// 30 minutes would get an inexplicable error.
	c.fetch = func(ctx context.Context, _ *http.Client, _ string) (*selfupdate.Release, error) {
		return nil, ctx.Err()
	}
	c.at = time.Now().Add(-releaseTTL - time.Second) // expire the cache to force an origin hit

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := c.get(ctx, nil, testRepo, false); err == nil {
		t.Fatal("the error should be passed through to a caller that canceled")
	}

	// Key invariant: the canceled call leaves no trace — the cache holds no
	// "canceled" error and still keeps the previous good result.
	if c.err != nil {
		t.Fatalf("a cancellation error must not be cached, got %v", c.err)
	}
	if c.rel == nil || c.rel.TagName != "v0.3.8" {
		t.Fatalf("the cache should keep the previous good result, got %+v", c.rel)
	}

	// The canceled call produced no new data, so the next visitor should hit the
	// origin again — and get a normal result, unaffected by the earlier cancellation.
	c.fetch = func(context.Context, *http.Client, string) (*selfupdate.Release, error) {
		return good, nil
	}
	rel, err := c.get(t.Context(), nil, testRepo, false)
	if err != nil {
		t.Fatalf("a normal request after the cancellation should not fail: %v", err)
	}
	if rel == nil || rel.TagName != "v0.3.8" {
		t.Fatalf("should get a normal result, got %+v", rel)
	}
}

func TestReleaseCachePeekNeverFetches(t *testing.T) {
	calls := 0
	c := newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return &selfupdate.Release{TagName: "v1.2.3"}, nil
	})
	if _, _, ok := c.peek(testRepo); ok {
		t.Fatal("empty cache must report nothing cached")
	}
	if calls != 0 {
		t.Fatalf("peek fetched %d times; it must never touch the network", calls)
	}
	if _, err := c.get(t.Context(), nil, testRepo, false); err != nil {
		t.Fatal(err)
	}
	rel, err, ok := c.peek(testRepo)
	if !ok || err != nil || rel.TagName != "v1.2.3" {
		t.Fatalf("peek after get = %v, %v, %v", rel, err, ok)
	}
	c.reset()
	if _, _, ok := c.peek(testRepo); ok {
		t.Fatal("reset must drop the cached result")
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

// TestUpdateEndpointsRequireOptIn pins the opt-in: with update checks off (the default) no
// endpoint reaches GitHub and installing is refused; once on, only an explicit check fetches.
func TestUpdateEndpointsRequireOptIn(t *testing.T) {
	t.Setenv(selfupdate.RepoEnv, "")
	m, err := NewManager(t.TempDir(), "")
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer m.Close()
	prev, hadPrev, _ := m.pg.GetSetting(settingUpdateCheck)
	defer func() {
		if hadPrev {
			_ = m.pg.SetSetting(settingUpdateCheck, prev)
		} else {
			_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingUpdateCheck)
		}
	}()
	_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingUpdateCheck)
	prevRepo, hadRepo, _ := m.pg.GetSetting(settingUpdateRepo)
	defer func() {
		if hadRepo {
			_ = m.pg.SetSetting(settingUpdateRepo, prevRepo)
		} else {
			_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingUpdateRepo)
		}
	}()
	_, _ = m.pg.Exec(`DELETE FROM settings WHERE key=$1`, settingUpdateRepo)

	calls := 0
	saved := relCache
	relCache = newTestCache(func(context.Context, *http.Client) (*selfupdate.Release, error) {
		calls++
		return &selfupdate.Release{TagName: "v9.9.9"}, nil
	})
	defer func() { relCache = saved }()

	s := New(context.Background(), m, t.TempDir(), t.TempDir(), t.TempDir())
	token, err := signJWT(s.jwtKey)
	if err != nil {
		t.Fatal(err)
	}
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		var buf bytes.Buffer
		if body != nil {
			_ = json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		s.Handler().ServeHTTP(rec, req)
		return rec
	}
	type checkResp struct {
		Enabled bool   `json:"enabled"`
		Repo    string `json:"repo"`
		Source  string `json:"repo_source"`
		Latest  string `json:"latest"`
		Error   string `json:"error"`
	}
	check := func(query string) checkResp {
		t.Helper()
		rec := do("GET", "/api/update/check?"+query, nil)
		if rec.Code != 200 {
			t.Fatalf("check?%s: status %d %s", query, rec.Code, rec.Body.String())
		}
		var v checkResp
		if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
			t.Fatal(err)
		}
		return v
	}

	// Off by default: even force=1 stays local, and apply is refused.
	if v := check("force=1"); v.Enabled || v.Latest != "" || v.Repo != selfupdate.DefaultRepo {
		t.Fatalf("default check = %+v; want disabled, no release, repo %s", v, selfupdate.DefaultRepo)
	}
	if rec := do("POST", "/api/update/apply", nil); rec.Code != 403 {
		t.Fatalf("apply while off: status %d, want 403", rec.Code)
	}
	if calls != 0 {
		t.Fatalf("GitHub fetched %d times while update checks were off", calls)
	}

	// On: cached=1 still never fetches; force=1 does, once.
	if rec := do("PUT", "/api/settings", map[string]any{"update_check_enabled": true}); rec.Code != 200 {
		t.Fatalf("enable: status %d %s", rec.Code, rec.Body.String())
	}
	if v := check("cached=1"); !v.Enabled || v.Latest != "" || calls != 0 {
		t.Fatalf("cached check before any explicit check = %+v (calls %d); want enabled, nothing fetched", v, calls)
	}
	if v := check("force=1"); v.Latest != "v9.9.9" || calls != 1 {
		t.Fatalf("explicit check = %+v (calls %d); want v9.9.9 after one fetch", v, calls)
	}
	if v := check("cached=1"); v.Latest != "v9.9.9" || calls != 1 {
		t.Fatalf("cached check after explicit check = %+v (calls %d)", v, calls)
	}

	// Off again: the earlier result is dropped and nothing is fetched.
	if rec := do("PUT", "/api/settings", map[string]any{"update_check_enabled": false}); rec.Code != 200 {
		t.Fatalf("disable: status %d", rec.Code)
	}
	if v := check("cached=1"); v.Enabled || v.Latest != "" {
		t.Fatalf("check after disabling = %+v; want disabled with no release", v)
	}
	if _, _, ok := relCache.peek(selfupdate.DefaultRepo); ok {
		t.Fatal("disabling must clear the cached release")
	}

	// The Settings field: saved values are validated, beat the environment, and can be cleared.
	t.Setenv(selfupdate.RepoEnv, "env/choice")
	if v := check("cached=1"); v.Repo != "env/choice" || v.Source != selfupdate.RepoFromEnv {
		t.Fatalf("env source = %+v", v)
	}
	if rec := do("PUT", "/api/settings", map[string]any{"update_repo": "not a repo"}); rec.Code != 400 {
		t.Fatalf("invalid update_repo: status %d, want 400", rec.Code)
	}
	if rec := do("PUT", "/api/settings", map[string]any{"update_repo": " ui/choice "}); rec.Code != 200 {
		t.Fatalf("save update_repo: status %d %s", rec.Code, rec.Body.String())
	}
	if v := check("cached=1"); v.Repo != "ui/choice" || v.Source != selfupdate.RepoFromSettings {
		t.Fatalf("settings source = %+v; want ui/choice from settings", v)
	}
	if rec := do("PUT", "/api/settings", map[string]any{"update_repo": ""}); rec.Code != 200 {
		t.Fatalf("clear update_repo: status %d", rec.Code)
	}
	t.Setenv(selfupdate.RepoEnv, "")
	if v := check("cached=1"); v.Repo != selfupdate.DefaultRepo || v.Source != selfupdate.RepoFromDefault {
		t.Fatalf("cleared source = %+v; want the default", v)
	}

	// An invalid source is reported and blocks the check instead of falling back to the default.
	t.Setenv(selfupdate.RepoEnv, "not a repo")
	if v := check("force=1"); v.Error == "" || calls != 1 {
		t.Fatalf("invalid repo check = %+v (calls %d); want an error and no fetch", v, calls)
	}
}

// A result cached for one source is never served for another: changing the source is a cache miss.
func TestReleaseCacheIsPerRepo(t *testing.T) {
	var asked []string
	c := &releaseCache{fetch: func(_ context.Context, _ *http.Client, repo string) (*selfupdate.Release, error) {
		asked = append(asked, repo)
		return &selfupdate.Release{TagName: "v1.0.0-" + repo}, nil
	}}
	if _, err := c.get(t.Context(), nil, "a/one", false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := c.peek("b/two"); ok {
		t.Fatal("peek must not serve a result cached for another source")
	}
	rel, err := c.get(t.Context(), nil, "b/two", false)
	if err != nil || rel.TagName != "v1.0.0-b/two" {
		t.Fatalf("get for the new source = %v, %v", rel, err)
	}
	if len(asked) != 2 || asked[0] != "a/one" || asked[1] != "b/two" {
		t.Fatalf("fetched %v; want one fetch per source", asked)
	}
}
