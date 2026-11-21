// Package selfupdate implements ARTEX's one-click update from the web UI: fetch the new binary from
// a GitHub Release, verify it, stage it, and swap it in atomically on the next startup.
//
// Division of labor (see start.sh / start.bat):
//
//	startup script = dumb supervisor loop; only decides "relaunch or not" from the exit code
//	this package   = all the error-prone logic (download / SHA256 check / smoke test / swap / rollback on failure)
//
// The swap lives in Go rather than in the scripts because the sha256 check and smoke test would need two
// implementations in sh and bat (sha256sum / shasum / certutil), and that is exactly the step that must not
// fail: swap in a binary that cannot run and the supervisor faithfully relaunches it forever, leaving manual repair.
//
// A full upgrade spans three process starts:
//
//  1. old server receives /api/update/apply → download + verify → stage artex.new → exit 75
//  2. script relaunches the old version → Bootstrap finds artex.new → verify + smoke → swap → exit 75
//  3. script relaunches, now the new version → Bootstrap counts an attempt → clears the marker after starting successfully
//
// Any failed step falls back to the old version: if step 2 fails verification, the staged build is deleted and the
// old version keeps running; if step 3 fails to reach marker clearing 3 times in a row, artex.old is swapped back.
package selfupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ExitRestart is the "supervisor, please relaunch me" exit code (EX_TEMPFAIL). The startup script reruns
// immediately on it without counting crash backoff. 0 means the user stopped normally (script exits its loop); anything else is a crash.
const ExitRestart = 75

// maxAttempts is the number of startup attempts allowed after a swap. Each start of the new version adds 1;
// surviving settleDelay clears the marker. Crashing maxAttempts times means it cannot start at all: roll back automatically.
const maxAttempts = 3

// Paths lists every file involved in an upgrade, all under the **executable's directory**.
// Deliberately not the CWD: when run as a service the working directory may be / or anything, which would put
// the staged build elsewhere and break the swap logic.
type Paths struct {
	Dir     string // directory containing the executable
	Current string // currently running binary    artex      / artex.exe
	New     string // staged new version          artex.new  / artex.new.exe
	Sum     string // sha256 (hex) of new version artex.new.sha256 / artex.new.exe.sha256
	Old     string // pre-swap backup             artex.old  / artex.old.exe
	Marker  string // upgrade state marker        artex.upgrade.json
}

// ResolvePaths derives all upgrade paths from the current executable.
//
// On Windows .new/.old must also end in .exe or the smoke test and post-swap exec fail,
// so strip the extension first and re-append it, keeping naming symmetric on both platforms.
func ResolvePaths() (Paths, error) {
	exe, err := os.Executable()
	if err != nil {
		return Paths{}, fmt.Errorf("failed to locate executable: %w", err)
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	dir := filepath.Dir(exe)
	name := filepath.Base(exe)
	ext := filepath.Ext(name) // ".exe" on Windows, usually empty on Unix
	stem := strings.TrimSuffix(name, ext)

	join := func(suffix string) string { return filepath.Join(dir, stem+suffix+ext) }
	return Paths{
		Dir:     dir,
		Current: exe,
		New:     join(".new"),
		Sum:     join(".new") + ".sha256",
		Old:     join(".old"),
		Marker:  filepath.Join(dir, stem+".upgrade.json"),
	}, nil
}

// marker records a swap's progress, used to trigger automatic rollback when the new version fails to start.
type marker struct {
	From     string `json:"from"`     // version before the upgrade
	To       string `json:"to"`       // target version
	Attempts int    `json:"attempts"` // startup attempts since the swap
	StagedAt int64  `json:"staged_at"`
}

func readMarker(path string) (marker, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return marker{}, false
	}
	var m marker
	if json.Unmarshal(b, &m) != nil {
		return marker{}, false
	}
	return m, true
}

func writeMarker(path string, m marker) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// cleanStaged removes the staged build. Used after successful swaps, failed verification and user cancel, so a
// leftover artex.new is not retried on the next startup.
func cleanStaged(p Paths) {
	_ = os.Remove(p.New)
	_ = os.Remove(p.Sum)
}

// CompareVersions compares two versions and returns -1/0/1 (a<b / a==b / a>b).
// ok=false means at least one side is not a comparable version (e.g. a local dev build "dev" or
// git describe output "0.3.7-2-gabc1234-dirty"); callers should then disable one-click update,
// or a development build would be "upgraded" to a release, overwriting uncommitted changes.
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	for i := range 3 {
		if av[i] != bv[i] {
			if av[i] < bv[i] {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

// parseVersion parses "v0.3.7" / "0.3.7" into [3]int.
//
// Only clean three-part versions are accepted: for non-tag builds build.sh uses git describe to produce
// suffixed versions like "0.3.7-2-gabc1234", which must be judged incomparable rather than treated as
// 0.3.7; otherwise dev builds would be judged "up to date" or overwritten by a release.
func parseVersion(s string) ([3]int, bool) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "v")
	if s == "" {
		return [3]int{}, false
	}
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return [3]int{}, false
	}
	var out [3]int
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return [3]int{}, false
		}
		out[i] = n
	}
	return out, true
}

// InDocker reports whether the process runs in a container. Under Docker the swap writes to the container's
// writable layer, and `docker compose up -d` recreating the container reverts to the image's version. That is
// expected (the user is pulling a new image then anyway), but the frontend must be able to explain it.
func InDocker() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	b, err := os.ReadFile("/proc/1/cgroup")
	if err != nil {
		return false
	}
	s := string(b)
	return strings.Contains(s, "docker") || strings.Contains(s, "containerd")
}
