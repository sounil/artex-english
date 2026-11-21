package server

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression test that keeps task_archive_package.go's archive-package delete-staging
// error message in English. The English check reuses assertEnglishError (no Hangul,
// Han, or kana). If anyone reverts this literal to Korean or Chinese, this test fails.
//
// Call-graph verdict (see the errArchiveStagedAndOriginalCoexist constant comment):
// the background archive worker runOneTaskArchiveJob (task_archives.go) stores this
// error in the task_archives.error column via FailTaskArchiveJob, and tasks/page.tsx's
// TaskArchivesPanel shows it directly as {archive.error}; it is user-facing only (no
// actool/planner feedback). Sibling errors on the same propagation path (archiveTask,
// validateArchivePath) are English as well, so this stays consistent with them.
//
// stageTaskArchivePackageDelete is a pure file function that never touches the DB, so
// on a DB-less host the test can create both the original file and the delete staging
// file (.deleting-N) to drive the real contradiction branch.
func TestTaskArchiveStagedAndOriginalCoexistErrorLocalized(t *testing.T) {
	dataDir := t.TempDir()
	archivePath := taskArchivePath(dataDir, 21, "42")
	if err := os.MkdirAll(filepath.Dir(archivePath), archiveDirMode); err != nil {
		t.Fatal(err)
	}
	// Create the contradictory state where both the original package and the delete staging file exist.
	if err := os.WriteFile(archivePath, []byte("original"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	staged := archivePath + ".deleting-21"
	if err := os.WriteFile(staged, []byte("archive"), archiveFileMode); err != nil {
		t.Fatal(err)
	}
	_, moved, err := stageTaskArchivePackageDelete(archivePath, 21)
	if err == nil {
		t.Fatalf("an error must be returned when the original and the delete staging file coexist (moved=%v)", moved)
	}
	if moved {
		t.Fatal("moved must not be true in the coexistence contradiction state")
	}
	assertEnglishError(t, "staged_and_original_coexist", err.Error())
}
