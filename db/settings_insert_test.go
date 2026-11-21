package db

import (
	"fmt"
	"testing"
	"time"
)

// InsertSettingIfAbsent is the safety net for auth.password_hash: the caller's GetSetting check
// can be defeated by a database error or overtaken by a concurrent request (bcrypt takes tens of
// milliseconds). So the "set only once" guarantee must rest on the primary key constraint, not on
// an if check.
func TestInsertSettingIfAbsentDoesNotOverwrite(t *testing.T) {
	d, err := Open(testDSN(t))
	if err != nil {
		t.Skipf("postgres unavailable (%v) — skipping", err)
	}
	defer d.Close()

	// Use a test-only key so the dev database's real auth.password_hash is left alone.
	key := fmt.Sprintf("test.insert_if_absent.%d", time.Now().UnixNano())
	defer func() { _, _ = d.Exec(`DELETE FROM settings WHERE key=$1`, key) }()

	inserted, err := d.InsertSettingIfAbsent(key, "first")
	if err != nil {
		t.Fatal(err)
	}
	if !inserted {
		t.Fatal("first write should return inserted=true")
	}

	inserted, err = d.InsertSettingIfAbsent(key, "second")
	if err != nil {
		t.Fatal(err)
	}
	if inserted {
		t.Fatal("should return inserted=false when the key already exists")
	}

	got, ok, err := d.GetSetting(key)
	if err != nil || !ok {
		t.Fatalf("GetSetting: ok=%v err=%v", ok, err)
	}
	if got != "first" {
		t.Fatalf("value was overwritten with %q; it should stay %q", got, "first")
	}
}
