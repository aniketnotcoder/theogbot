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

func TestDownloadLimitIsBounded(t *testing.T) {
	if MaxDownloadBytes <= 0 || MaxDownloadBytes > 2<<30 {
		t.Fatalf("unexpected download limit: %d", MaxDownloadBytes)
	}
}

func TestSafeFilename(t *testing.T) {
	cases := map[string]string{
		"../../secret.txt": "secret.txt",
		`C:\private\video.mp4`: "video.mp4",
		"": "telegram-file",
		"..": "telegram-file",
		"folder/": "folder",
		"  report.pdf  ": "report.pdf",
	}
	for input, want := range cases {
		if got := safeFilename(input); got != want { t.Errorf("safeFilename(%q) = %q, want %q", input, got, want) }
	}
}
