package wistia

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
)

func TestDownloadWriterAtConcurrentChunks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "video.mp4")
	f, err := os.Create(path)
	if err != nil { t.Fatal(err) }
	defer f.Close()
	var callbacks atomic.Int64
	w := &downloadWriterAt{
		file: f,
		total: 8,
		progress: func(stage string, current, total int64) {
			if stage != "Downloading" || total != 8 { t.Errorf("unexpected progress: %s %d/%d", stage, current, total) }
			callbacks.Add(1)
		},
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, chunk := range []struct{ off int64; data string }{{0, "1234"}, {4, "5678"}} {
		wg.Add(1)
		go func(off int64, data string) {
			defer wg.Done()
			_, writeErr := w.WriteAt([]byte(data), off)
			errs <- writeErr
		}(chunk.off, chunk.data)
	}
	wg.Wait()
	close(errs)
	for err := range errs { if err != nil { t.Fatal(err) } }
	got := make([]byte, 8)
	if _, err := f.ReadAt(got, 0); err != nil { t.Fatal(err) }
	if string(got) != "12345678" { t.Fatalf("file contents = %q", got) }
	if w.written.Load() != 8 || callbacks.Load() != 2 {
		t.Fatalf("written=%d progress callbacks=%d", w.written.Load(), callbacks.Load())
	}
}

func TestDownloadWriterAtRejectsOutOfBoundsChunk(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "video-*.mp4")
	if err != nil { t.Fatal(err) }
	defer f.Close()
	w := &downloadWriterAt{file:f,total:4}
	if _, err := w.WriteAt([]byte("12345"),0); err == nil {
		t.Fatal("expected chunk larger than declared size to fail")
	}
	if w.written.Load() != 0 { t.Fatalf("unexpected byte count: %d", w.written.Load()) }
}
