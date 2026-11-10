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

func TestProgressWriterAtCountsParallelWrites(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "progress-*")
	if err != nil { t.Fatal(err) }
	defer f.Close()
	var got int64
	w := &progressWriterAt{file:f,total:6,callback:func(current,total int64){ got=current; if total!=6 { t.Errorf("total=%d, want 6",total) } }}
	if _,err:=w.WriteAt([]byte("abc"),3);err!=nil{t.Fatal(err)}
	if _,err:=w.WriteAt([]byte("def"),0);err!=nil{t.Fatal(err)}
	if got!=6 || w.written.Load()!=6 { t.Fatalf("progress=%d written=%d, want 6",got,w.written.Load()) }
}

func TestEnoughDiskSpaceIncludesReserveAndRejectsOversize(t *testing.T) {
	if !enoughDiskSpace(minFreeDiskBytes+1024,1024) { t.Fatal("expected exact required space to pass") }
	if enoughDiskSpace(minFreeDiskBytes+1023,1024) { t.Fatal("expected insufficient reserve to fail") }
	if enoughDiskSpace(4<<30,MaxDownloadBytes+1) { t.Fatal("expected oversize to fail") }
}

func TestTempFilePatternUsesJPGForPhotos(t *testing.T) {
	if got := tempFilePattern("photo"); got != "theogbot-*.jpg" {
		t.Fatalf("photo temp pattern = %q, want .jpg suffix", got)
	}
	if got := tempFilePattern("document"); got != "theogbot-*" {
		t.Fatalf("document temp pattern = %q, want unchanged generic pattern", got)
	}
	if got := tempFilePattern("video"); got != "theogbot-*" {
		t.Fatalf("video temp pattern = %q, want unchanged generic pattern", got)
	}
}

func TestDownloadWorkerCountReadsBoundedRailwaySetting(t *testing.T) {
	t.Setenv("THEOGBOT_DOWNLOAD_THREADS","16")
	if got:=downloadWorkerCount();got!=16 { t.Fatalf("downloadWorkerCount()=%d, want 16",got) }
	t.Setenv("THEOGBOT_DOWNLOAD_THREADS","0")
	if got:=downloadWorkerCount();got!=8 { t.Fatalf("invalid download worker count=%d, want default 8",got) }
}
