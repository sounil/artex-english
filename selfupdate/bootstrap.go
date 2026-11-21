package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"
)

// smokeEnv makes the child process launched by the smoke test skip Bootstrap.
//
// Strictly speaking nothing breaks without it: the child's os.Executable() is artex.new,
// so every derived path carries the .new prefix and cannot touch the real upgrade files.
// Relying on that coincidence is fragile, though; an explicit short-circuit is clearer.
const smokeEnv = "ARTEX_SELFUPDATE_SMOKE"

// Action is Bootstrap's instruction to main.
type Action int

const (
	// Continue: start the server as usual.
	Continue Action = iota
	// Restart: exit immediately with ExitRestart so the supervisor script relaunches us.
	Restart
)

// State describes the upgrade state at this startup, so /api/update/check can tell the
// frontend truthfully whether the last upgrade succeeded or was rolled back.
type State struct {
	Pending     bool   // swapped in but not yet confirmed stable
	RolledBack  bool   // this startup just performed an automatic rollback
	FailedStage bool   // staged build failed verification/smoke test and was discarded
	Detail      string // one-sentence user-facing explanation
}

// Bootstrap runs at the very start of main and must be called before listening on any port or opening the database.
//
// Three situations:
//
//  1. staged artex.new exists  → verify + smoke test; on success swap and request a restart, otherwise discard and keep running the old version
//  2. only the marker remains  → we were just swapped in; count one attempt, roll back after too many consecutive failures
//  3. nothing                  → normal startup
func Bootstrap() (Action, State) {
	if os.Getenv(smokeEnv) != "" {
		return Continue, State{}
	}
	p, err := ResolvePaths()
	if err != nil {
		log.Printf("[update] skipping bootstrap: %v", err)
		return Continue, State{}
	}

	if _, err := os.Stat(p.New); err == nil {
		return applyStaged(p)
	}

	m, ok := readMarker(p.Marker)
	if !ok {
		return Continue, State{}
	}
	return confirmOrRollback(p, m)
}

// applyStaged handles the "staged build exists" case: swap on successful verification, discard on failure.
//
// This is the only place in the upgrade chain that overwrites the executable, and the last gate: the smoke
// test blocks corrupt downloads, wrong architectures and missing dynamic libraries. Once a binary that cannot
// run is let through, the supervisor relaunches it tirelessly, Go code never runs, and automatic rollback is impossible.
func applyStaged(p Paths) (Action, State) {
	m, _ := readMarker(p.Marker)

	if err := verifyStaged(p); err != nil {
		log.Printf("[update] staged version failed verification, discarded; continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "New version failed verification and was discarded: " + err.Error()}
	}

	if err := swap(p); err != nil {
		log.Printf("[update] version swap failed, continuing on current version: %v", err)
		cleanStaged(p)
		_ = os.Remove(p.Marker)
		return Continue, State{FailedStage: true, Detail: "Failed to swap versions: " + err.Error()}
	}

	// Swap succeeded. Keep the marker; the next startup (running the new version) confirms whether it is stable.
	m.Attempts = 0
	if m.StagedAt == 0 {
		m.StagedAt = time.Now().Unix()
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to write upgrade marker (automatic rollback unavailable): %v", err)
	}
	log.Printf("[update] swapped to %s, exiting to restart (exit %d)", orUnknown(m.To), ExitRestart)
	return Restart, State{Pending: true}
}

// confirmOrRollback handles "startup after a swap": count attempts and swap the old version back once over the limit.
//
// The counter only increments once Go code is running, so it covers failures of the "executes but crashes
// during init" kind (incompatible config, port in use, failed DB migration); "cannot exec at all" is blocked by
// the pre-swap smoke test. Together the two are complete.
func confirmOrRollback(p Paths, m marker) (Action, State) {
	m.Attempts++
	if m.Attempts > maxAttempts {
		if err := rollback(p); err != nil {
			// If even the rollback failed, stop restarting or we loop forever. Clear the marker and
			// start in the current state; if that fails, at least the user can see why in the logs.
			log.Printf("[update] new version failed to start %d times in a row and rollback failed: %v", maxAttempts, err)
			_ = os.Remove(p.Marker)
			return Continue, State{Detail: "New version failed to start and the rollback also failed: " + err.Error()}
		}
		log.Printf("[update] new version failed to start %d times in a row, rolled back to %s, exiting to restart (exit %d)",
			maxAttempts, orUnknown(m.From), ExitRestart)
		_ = os.Remove(p.Marker)
		return Restart, State{RolledBack: true, Detail: fmt.Sprintf("New version failed to start; rolled back to the previous version (%s)", orUnknown(m.From))}
	}
	if err := writeMarker(p.Marker, m); err != nil {
		log.Printf("[update] failed to update upgrade marker: %v", err)
	}
	log.Printf("[update] starting new version (attempt %d/%d), upgrade is confirmed once it runs stably",
		m.Attempts, maxAttempts)
	return Continue, State{Pending: true}
}

// Settle confirms the new version runs stably and clears the upgrade marker.
//
// main calls it after a delay once HTTP is listening: only surviving that long counts. Otherwise the marker
// stays put and the next startup keeps counting attempts until a rollback triggers.
func Settle() {
	p, err := ResolvePaths()
	if err != nil {
		return
	}
	settle(p)
}

func settle(p Paths) {
	if _, ok := readMarker(p.Marker); !ok {
		return // not a post-upgrade startup, nothing to do
	}
	if err := os.Remove(p.Marker); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("[update] failed to clear upgrade marker: %v", err)
		return
	}
	log.Printf("[update] new version is stable, upgrade complete (previous version kept as %s)", p.Old)
}

// SettleDelay is how long the new version must run to count as "survived".
const SettleDelay = 30 * time.Second

// verifyStaged verifies the staged build: compare SHA256 first, then actually launch it once.
func verifyStaged(p Paths) error {
	want, err := os.ReadFile(p.Sum)
	if err != nil {
		return fmt.Errorf("failed to read checksum: %w", err)
	}
	got, err := fileSHA256(p.New)
	if err != nil {
		return fmt.Errorf("failed to compute checksum: %w", err)
	}
	if !strings.EqualFold(strings.TrimSpace(string(want)), got) {
		return errors.New("SHA256 mismatch (download corrupted or tampered with)")
	}
	return smokeTest(p.New)
}

// smokeTest launches the new binary with -h to confirm it actually executes on this system.
// This blocks a whole class of problems: truncated downloads, wrong architecture (exec format error), missing dependencies.
func smokeTest(bin string) error {
	if err := os.Chmod(bin, 0o755); err != nil {
		return fmt.Errorf("failed to set execute permission: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "-h")
	cmd.Env = append(os.Environ(), smokeEnv+"=1")
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return errors.New("smoke test timed out (new binary did not respond)")
	}
	if err != nil {
		snippet := strings.TrimSpace(string(out))
		if len(snippet) > 300 {
			snippet = snippet[:300] + "…"
		}
		return fmt.Errorf("smoke test failed: %v: %s", err, snippet)
	}
	return nil
}

// swap replaces the current binary with the staged new version.
//
// Both Unix and Windows allow renaming a running executable (Windows forbids deleting and
// overwriting it, not renaming), so no per-platform code and no need to stop ourselves first.
func swap(p Paths) error {
	// Rename on Windows does not overwrite an existing target, so a .old left by a previous upgrade must be removed first.
	if err := os.Remove(p.Old); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("failed to clean up previous backup %s: %w", p.Old, err)
	}
	if err := os.Rename(p.Current, p.Old); err != nil {
		return fmt.Errorf("failed to back up current version: %w", err)
	}
	if err := os.Rename(p.New, p.Current); err != nil {
		// Install failed but the current version was already moved away; put it back, or the next startup has no executable.
		if rerr := os.Rename(p.Old, p.Current); rerr != nil {
			return fmt.Errorf("failed to install new version (%v), and restoring the current version also failed: %w", err, rerr)
		}
		return fmt.Errorf("failed to install new version: %w", err)
	}
	_ = os.Remove(p.Sum)
	return nil
}

// rollback swaps back the old version that swap backed up.
func rollback(p Paths) error {
	if _, err := os.Stat(p.Old); err != nil {
		return fmt.Errorf("no backup to roll back to (%s): %w", p.Old, err)
	}
	// Move the new version that failed to start to .failed for troubleshooting rather than deleting it.
	failed := p.Current + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(p.Current, failed); err != nil {
		return fmt.Errorf("failed to move the failed version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		return fmt.Errorf("failed to restore previous version: %w", err)
	}
	return nil
}

// Rollback implements /api/update/rollback: actively revert to the previous version.
// It only swaps; restarting is again left to the supervisor script (the caller then exits with ExitRestart).
func Rollback() error {
	p, err := ResolvePaths()
	if err != nil {
		return err
	}
	if _, err := os.Stat(p.Old); err != nil {
		return errors.New("no previous version to roll back to (" + p.Old + " not found)")
	}
	cleanStaged(p)
	if err := smokeTest(p.Old); err != nil {
		return fmt.Errorf("refusing to roll back: the previous version cannot run: %w", err)
	}
	// Swap current and backup, so a rollback can itself be rolled back.
	tmp := p.Current + ".swap"
	_ = os.Remove(tmp)
	if err := os.Rename(p.Current, tmp); err != nil {
		return fmt.Errorf("failed to move current version: %w", err)
	}
	if err := os.Rename(p.Old, p.Current); err != nil {
		_ = os.Rename(tmp, p.Current)
		return fmt.Errorf("failed to install previous version: %w", err)
	}
	if err := os.Rename(tmp, p.Old); err != nil {
		log.Printf("[update] failed to tidy backups after rollback (no runtime impact): %v", err)
	}
	_ = os.Remove(p.Marker)
	return nil
}

// HasBackup reports whether a previous version exists to roll back to, so the frontend can decide whether to show the rollback button.
func HasBackup() bool {
	p, err := ResolvePaths()
	if err != nil {
		return false
	}
	_, err = os.Stat(p.Old)
	return err == nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func orUnknown(s string) string {
	if strings.TrimSpace(s) == "" {
		return "unknown version"
	}
	return s
}
