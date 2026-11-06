package media

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCleanupRemovesDownloadedFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sample.bin")
	if err := os.WriteFile(path, []byte("data"), 0600); err != nil { t.Fatal(err) }
	d := &Downloaded{Path:path}
	if err := d.Cleanup(); err != nil { t.Fatal(err) }
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) { t.Fatalf("expected removal, got %v", err) }
}

func TestCleanupNilSafe(t *testing.T) {
	var d *Downloaded
	if err := d.Cleanup(); err != nil { t.Fatal(err) }
}

func TestProtectedError(t *testing.T) {
	if ErrProtectedMessage == nil || ErrProtectedMessage.Error() == "" { t.Fatal("protected-content diagnostic must be defined") }
}
