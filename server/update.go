package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/Autumn-27/artex/selfupdate"
)

// HTTP surface of the in-page one-click update. The real download/verify/swap logic lives in the selfupdate package;
// this only handles the auth boundary, mutual exclusion, progress broadcast, and telling main "time to exit".
//
// The restart is not done by this process: after staging the new version the process exits with selfupdate.ExitRestart
// and the supervisor script (start.sh / start.bat, or the ENTRYPOINT under Docker) starts it again.

// User-facing update messages (SSE progress messages, check reasons, error responses).
// The frontend (update-card in system/settings) renders progress.message, reason and the writeErr
// body verbatim.
const (
	updateMsgPreparing          = "Preparing…"
	updateMsgFailed             = "Update failed"
	updateMsgStaged             = "The new version is ready. Restarting…"
	updateErrNotReleaseFmt      = "The current version %q is not an official release, so one-click update is unavailable"
	updateErrAlreadyLatestFmt   = "Already on the latest version (%s)"
	updateErrInProgress         = "An update is already in progress"
	updateErrRollbackInProgress = "Cannot roll back while an update is in progress"
	updateErrDisabled           = "Updates are turned off. Turn on update checks in Settings to check for and install new versions"
)

// settingUpdateCheck is the opt-in for everything that talks to GitHub: checking for a new release and
// installing it. Off by default, so a fresh install never contacts the update source on its own.
const settingUpdateCheck = "update_check_enabled"

// settingUpdateRepo is the release source ("owner/name") chosen in Settings. Empty means "use
// ARTEX_UPDATE_REPO, else selfupdate.DefaultRepo" (see selfupdate.ResolveRepo).
const settingUpdateRepo = "update_repo"

// updatesEnabled reports whether the operator opted in to update checks.
func (s *Server) updatesEnabled() bool {
	return s.m.pg.GetBool(settingUpdateCheck, false)
}

// updateRepoSetting returns the raw Settings value for the release source ("" when unset).
func (s *Server) updateRepoSetting() string {
	v, _, _ := s.m.pg.GetSetting(settingUpdateRepo)
	return strings.TrimSpace(v)
}

// updateRepo returns the effective release source and where it came from.
func (s *Server) updateRepo() (repo, source string, err error) {
	return selfupdate.ResolveRepo(s.updateRepoSetting())
}

// restartCh is closed once the upgrade is ready or a rollback completes; main then exits with ExitRestart.
var (
	restartOnce sync.Once
	restartCh   = make(chan struct{})
)

// RestartRequested returns a channel that is closed when "please exit and let the supervisor restart me".
func RestartRequested() <-chan struct{} { return restartCh }

func requestRestart() { restartOnce.Do(func() { close(restartCh) }) }

// bootState is selfupdate.Bootstrap's verdict for this startup (upgrade succeeded / just rolled back /
// staged file discarded), injected by main so /api/update/check can tell the frontend truthfully how the last upgrade ended.
var (
	bootStateMu sync.Mutex
	bootState   selfupdate.State
)

// SetBootUpdateState is called once by main at startup.
func SetBootUpdateState(st selfupdate.State) {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	bootState = st
}

func bootUpdateState() selfupdate.State {
	bootStateMu.Lock()
	defer bootStateMu.Unlock()
	return bootState
}

// releaseCache caches the result of the GitHub latest-release query.
//
// The top bar's "new version available" hint queries on every full page load, and the unauthenticated GitHub API allows
// 60 requests per IP per hour — without caching, a few tabs or refreshes would exhaust the quota,
// and a real update check would then fail. An explicit "Check for updates" click can force-bypass the cache.
type releaseCache struct {
	mu   sync.Mutex
	repo string // source the cached result came from; a different source is a cache miss
	rel  *selfupdate.Release
	err  error
	at   time.Time
	// fetch is the fetch function, an injection point for tests only; nil means the real GitHub query.
	fetch func(ctx context.Context, client *http.Client, repo string) (*selfupdate.Release, error)
}

// fresh reports whether the cached entry is for repo and still within its TTL. Caller holds mu.
func (c *releaseCache) fresh(repo string) bool {
	ttl := releaseTTL
	if c.err != nil {
		ttl = releaseErrTTL
	}
	return !c.at.IsZero() && c.repo == repo && time.Since(c.at) < ttl
}

const (
	releaseTTL = 30 * time.Minute
	// Failures are cached briefly too, otherwise every page load would wait out a timeout while GitHub is unreachable;
	// but the TTL is short so it recovers quickly once the network is back.
	releaseErrTTL = 2 * time.Minute
	// Timeout for the query. NewClient's 30-minute timeout is for downloading the whole package; a version check cannot wait that long.
	releaseTimeout = 20 * time.Second
)

var relCache = &releaseCache{}

// peek returns the cached result for repo without ever touching the network. ok is false when nothing
// is cached for that source or the entry has expired. The top-bar badge uses this, so opening a page
// never triggers a GitHub call.
func (c *releaseCache) peek(repo string) (rel *selfupdate.Release, err error, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.fresh(repo) {
		return nil, nil, false
	}
	return c.rel, c.err, true
}

// reset drops the cached result, e.g. when update checks are turned off, so a stale
// "new version available" never outlives the opt-in.
func (c *releaseCache) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.repo, c.rel, c.err, c.at = "", nil, nil, time.Time{}
}

// get returns the latest Release, without network access on a cache hit.
//
// The lock is held during the fetch: concurrent requests queue for the result of the same query instead of each hitting GitHub
// (several tabs querying right after page load is exactly when rate limiting is most likely).
func (c *releaseCache) get(ctx context.Context, client *http.Client, repo string, force bool) (*selfupdate.Release, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !force && c.fresh(repo) {
		return c.rel, c.err
	}

	fetch := c.fetch
	if fetch == nil {
		fetch = selfupdate.FetchLatest
	}
	ctx, cancel := context.WithTimeout(ctx, releaseTimeout)
	defer cancel()
	rel, err := fetch(ctx, client, repo)
	// A cancelled request (user closed the tab) does not mean GitHub has a problem; do not cache it,
	// or the next visitor would get a baffling "cancelled" error.
	if err != nil && ctx.Err() != nil && errors.Is(ctx.Err(), context.Canceled) {
		if c.repo != repo {
			return nil, err // never hand back another source's release
		}
		return c.rel, err
	}
	c.repo, c.rel, c.err, c.at = repo, rel, err, time.Now()
	return rel, err
}

// updateProgress is one progress update pushed to the frontend.
type updateProgress struct {
	Phase   selfupdate.Phase `json:"phase"`
	Percent int              `json:"percent"` // only meaningful during download; -1 otherwise
	Message string           `json:"message"`
	Version string           `json:"version,omitempty"`
	Error   string           `json:"error,omitempty"`
}

// updateHub holds the progress of one upgrade and broadcasts it to SSE subscribers.
//
// running doubles as mutual exclusion: another POST /api/update/apply during an upgrade gets 409,
// so two goroutines never write the same artex.new.
type updateHub struct {
	mu      sync.Mutex
	running bool
	cur     updateProgress
	subs    map[chan updateProgress]struct{}
}

var updHub = &updateHub{
	cur:  updateProgress{Phase: selfupdate.PhaseIdle, Percent: -1},
	subs: map[chan updateProgress]struct{}{},
}

// begin claims the upgrade slot; returns false if one is already running.
func (h *updateHub) begin(version string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.running {
		return false
	}
	h.running = true
	h.cur = updateProgress{Phase: selfupdate.PhaseDownload, Percent: 0, Message: updateMsgPreparing, Version: version}
	h.fanout(h.cur)
	return true
}

// finish ends an upgrade. A nil err means staging succeeded and a restart is pending.
func (h *updateHub) finish(err error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	if err != nil {
		h.cur = updateProgress{Phase: selfupdate.PhaseFailed, Percent: -1, Message: updateMsgFailed, Error: err.Error(), Version: h.cur.Version}
	} else {
		h.cur = updateProgress{Phase: selfupdate.PhaseStaged, Percent: 100, Message: updateMsgStaged, Version: h.cur.Version}
	}
	h.fanout(h.cur)
}

func (h *updateHub) publish(ph selfupdate.Phase, pct int, msg string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.cur = updateProgress{Phase: ph, Percent: pct, Message: msg, Version: h.cur.Version}
	h.fanout(h.cur)
}

// fanout must be called with h.mu held. Subscriber channels are buffered and drop when full —
// progress is disposable transient info, and a stuck SSE connection must never block the upgrade itself.
func (h *updateHub) fanout(p updateProgress) {
	for ch := range h.subs {
		select {
		case ch <- p:
		default:
		}
	}
}

func (h *updateHub) snapshot() (updateProgress, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.cur, h.running
}

func (h *updateHub) subscribe() (<-chan updateProgress, func()) {
	ch := make(chan updateProgress, 64)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			h.mu.Unlock()
			close(ch)
		})
	}
}

// updateCheck queries GitHub for the latest official release and compares it with the current version.
//
// Nothing here contacts GitHub unless the operator turned on update checks (settingUpdateCheck), and even then
// only an explicit check does: ?cached=1 (the top-bar badge and the settings card on load) answers from the
// cache alone, and ?force=1 (the "Check for updates" button) fetches fresh.
//
// The frontend also talks to api.github.com directly (GitHub's CORS is *), but **this endpoint is authoritative**:
// the backend does the download, so an update is only possible if the backend can reach GitHub. It is common for the browser
// to connect while the server cannot (server on an internal network, or a proxy configured only in the browser); updating would then fail,
// so it is better to report the error honestly at the check step.
func (s *Server) updateCheck(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion
	mode := "binary"
	if selfupdate.InDocker() {
		mode = "docker"
	}
	boot := bootUpdateState()
	out := map[string]any{
		"current":      current,
		"mode":         mode,
		"os":           runtime.GOOS,
		"arch":         runtime.GOARCH,
		"has_backup":   selfupdate.HasBackup(),
		"boot_notice":  boot.Detail,
		"rolled_back":  boot.RolledBack,
		"enabled":      s.updatesEnabled(),
		"repo_env":     selfupdate.RepoEnv,
		"repo_setting": s.updateRepoSetting(),
	}
	repo, source, repoErr := s.updateRepo()
	out["repo"] = repo
	out["repo_source"] = source
	if repoErr != nil {
		out["error"] = repoErr.Error()
		writeJSON(w, 200, out)
		return
	}
	if !s.updatesEnabled() {
		writeJSON(w, 200, out) // opted out: report local state only, never contact GitHub
		return
	}

	q := r.URL.Query()
	var (
		rel *selfupdate.Release
		err error
	)
	if q.Get("cached") != "" {
		var ok bool
		if rel, err, ok = relCache.peek(repo); !ok {
			writeJSON(w, 200, out) // nothing checked yet; wait for an explicit check
			return
		}
	} else {
		// Clicking "Check for updates" sends force=1 to bypass the cache.
		client := selfupdate.NewClient(s.m.GlobalProxy())
		rel, err = relCache.get(r.Context(), client, repo, q.Get("force") != "")
	}
	if err != nil {
		out["error"] = err.Error()
		writeJSON(w, 200, out)
		return
	}

	latest := rel.TagName
	out["latest"] = latest
	out["notes"] = rel.Body
	out["html_url"] = rel.HTMLURL
	if !rel.PublishedAt.IsZero() {
		out["published_at"] = rel.PublishedAt.Format(time.RFC3339)
	}

	asset := selfupdate.AssetName(latest, runtime.GOOS, runtime.GOARCH)
	out["asset"] = asset
	if a, ok := rel.FindAsset(asset); ok {
		out["asset_available"] = true
		out["size"] = a.Size
	} else {
		out["asset_available"] = false
	}

	cmp, comparable := selfupdate.CompareVersions(current, latest)
	out["comparable"] = comparable
	out["has_update"] = comparable && cmp < 0
	if !comparable {
		// Dev builds (dev / git describe with a suffix) have no comparable version. Allowing it would only
		// overwrite the binary being debugged locally with a release, so updates are refused outright.
		out["reason"] = fmt.Sprintf(updateErrNotReleaseFmt, current)
	}
	writeJSON(w, 200, out)
}

// updateApply downloads and stages the new version, then lets the process exit for the supervisor script to restart.
//
// It returns 202 immediately and does the work on a background goroutine: downloading the whole package may take minutes,
// and hanging on the request would be cut off by reverse proxy timeouts. Progress goes through /api/update/stream.
func (s *Server) updateApply(w http.ResponseWriter, r *http.Request) {
	current := BuildVersion
	if !s.updatesEnabled() {
		writeErr(w, 403, updateErrDisabled)
		return
	}

	// Use the cache: ensures the installed version is the one the user saw and confirmed in the UI.
	client := selfupdate.NewClient(s.m.GlobalProxy())
	repo, _, err := s.updateRepo()
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	rel, err := relCache.get(r.Context(), client, repo, false)
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	cmp, comparable := selfupdate.CompareVersions(current, rel.TagName)
	if !comparable {
		writeErr(w, 400, fmt.Sprintf(updateErrNotReleaseFmt, current))
		return
	}
	if cmp >= 0 {
		writeErr(w, 400, fmt.Sprintf(updateErrAlreadyLatestFmt, current))
		return
	}
	if !updHub.begin(rel.TagName) {
		writeErr(w, 409, updateErrInProgress)
		return
	}

	go func() {
		// Deliberately use s.ctx rather than the request ctx: the request ends as soon as the HTTP response returns,
		// and a download tied to it would be cancelled immediately.
		err := selfupdate.Stage(s.ctx, client, rel, current, func(ph selfupdate.Phase, pct int, msg string) {
			updHub.publish(ph, pct, msg)
		})
		updHub.finish(err)
		if err != nil {
			log.Printf("[update] update failed: %v", err)
			return
		}
		log.Printf("[update] %s -> %s staged, exiting to complete the swap", current, rel.TagName)
		// Leave a moment to push the last progress update to the frontend before triggering the exit.
		time.Sleep(1500 * time.Millisecond)
		requestRestart()
	}()

	writeJSON(w, 202, map[string]any{"ok": true, "target": rel.TagName})
}

// updateRollback actively reverts to the previous version (artex.old, backed up before the swap).
func (s *Server) updateRollback(w http.ResponseWriter, r *http.Request) {
	if _, running := updHub.snapshot(); running {
		writeErr(w, 409, updateErrRollbackInProgress)
		return
	}
	if err := selfupdate.Rollback(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	log.Printf("[update] manually rolled back to the previous version, exiting to complete the switch")
	writeJSON(w, 202, map[string]any{"ok": true})
	go func() {
		time.Sleep(500 * time.Millisecond)
		requestRestart()
	}()
}

// updateStream pushes update progress over SSE.
func (s *Server) updateStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, 500, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	ch, unsub := updHub.subscribe()
	defer unsub()

	send := func(p updateProgress) {
		b, _ := json.Marshal(p)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	// Send the current state first so a refreshed page immediately sees an upgrade in progress.
	cur, _ := updHub.snapshot()
	send(cur)

	ctx := r.Context()
	ping := time.NewTicker(20 * time.Second)
	defer ping.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case p, ok := <-ch:
			if !ok {
				return
			}
			send(p)
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}
