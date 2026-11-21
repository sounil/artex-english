package server

import (
	"fmt"
	"strings"
	"testing"
)

// TestSkillZipErrorsLocalized guards the F3b skill_zip.go bundle: every user-facing
// skill-upload error response (fsUploadSkill surfaces these via
// writeErr(400, err.Error()), see server_mgmt.go) must be English, and no zip-method
// display name may stay Korean or Chinese. Reverting any of these literals fails here.
func TestSkillZipErrorsLocalized(t *testing.T) {
	// Message constants rendered with sample arguments.
	assertEnglishError(t, "errSkillZipParse",
		fmt.Errorf(errSkillZipParse, fmt.Errorf("boom")).Error())
	assertEnglishError(t, "errSkillZipEncrypted",
		fmt.Sprintf(errSkillZipEncrypted, "demo/SKILL.md"))
	assertEnglishError(t, "errSkillZipUnsupported",
		fmt.Sprintf(errSkillZipUnsupported, "LZMA", 14, "demo/SKILL.md"))

	// A non-zip upload must reach the user as an English hint, not a raw stdlib error.
	if _, err := newSkillZipReader([]byte("this is not a zip archive")); err == nil {
		t.Fatal("non-zip input must return an error")
	} else {
		assertEnglishError(t, "newSkillZipReader", err.Error())
		if !strings.Contains(err.Error(), "archive") {
			t.Fatalf("parse error = %q, want an 'archive' hint", err.Error())
		}
	}

	// Every zip-method display name, including the two translated ones (AES,
	// unknown fallback), must be English.
	for m, name := range zipMethodNames {
		assertEnglishError(t, fmt.Sprintf("zipMethodNames[%d]", m), name)
	}
	assertEnglishError(t, "zipMethodName(AES)", zipMethodName(zipMethodAES))
	assertEnglishError(t, "zipMethodName(unknown)", zipMethodName(0xffff))
}
