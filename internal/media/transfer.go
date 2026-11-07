package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"io"
	"sync/atomic"
	"syscall"
	"path"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

var ErrProtectedMessage = errors.New("Telegram marks this message as protected (noforwards); download was not attempted")

type ProgressFunc func(current, total int64)

type progressWriterAt struct { file *os.File; written atomic.Int64; total int64; callback ProgressFunc }

func (w *progressWriterAt) WriteAt(p []byte, off int64) (int, error) {
	current := w.written.Add(int64(len(p)))
	if current > MaxDownloadBytes { w.written.Add(-int64(len(p))); return 0, fmt.Errorf("download exceeded the configured %d-byte limit", MaxDownloadBytes) }
	n, err := w.file.WriteAt(p, off)
	if n < len(p) { w.written.Add(int64(n)-int64(len(p))) }
	if n > 0 && w.callback != nil { w.callback(w.written.Load(), w.total) }
	if err == nil && n != len(p) { err = io.ErrShortWrite }
	return n, err
}

const minFreeDiskBytes int64 = 128 << 20

func enoughDiskSpace(available, expected int64) bool {
	if expected < 0 || expected > MaxDownloadBytes { return false }
	return available >= expected+minFreeDiskBytes
}

func checkDiskSpace(dir string, expected int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(dir, &stat); err != nil { return fmt.Errorf("check temporary disk space: %w", err) }
	available := int64(stat.Bavail) * int64(stat.Bsize)
	if !enoughDiskSpace(available, expected) { return fmt.Errorf("insufficient temporary disk space: need at least %d bytes including reserve, have %d", expected+minFreeDiskBytes, available) }
	return nil
}

type Downloaded struct {
	Path, Name, MIME, Kind, Caption string
	GroupedID int64
	Entities []tg.MessageEntityClass
	Size int64
	Attributes []tg.DocumentAttributeClass
}

const MaxDownloadBytes int64 = 2 << 30

// DownloadMessage downloads an accessible, non-protected photo or document.
// Telegram remains authoritative about whether a media location can be retrieved.
func DownloadMessage(ctx context.Context, api *tg.Client, elem messages.Elem, tempDir string) (*Downloaded, error) {
	return DownloadMessageWithProgress(ctx, api, elem, tempDir, nil)
}

func DownloadMessageWithProgress(ctx context.Context, api *tg.Client, elem messages.Elem, tempDir string, progress ProgressFunc) (_ *Downloaded, err error) {
	if api == nil { return nil, errors.New("source MTProto API is required") }
	msg, ok := elem.Msg.(*tg.Message)
	if !ok { return nil, fmt.Errorf("unsupported message type %T", elem.Msg) }
	if msg.Noforwards { return nil, ErrProtectedMessage }
	file, ok := elem.File()
	if !ok { return nil, errors.New("message has no photo or document attachment") }

	kind := "document"
	var groupedID int64
	if msg.GroupedID != 0 { groupedID = msg.GroupedID }
	var attributes []tg.DocumentAttributeClass
	if _, isPhoto := elem.Photo(); isPhoto {
		kind = "photo"
	} else if doc, isDocument := elem.Document(); isDocument {
		if doc.Size > MaxDownloadBytes { return nil, fmt.Errorf("media is larger than the configured %d-byte limit", MaxDownloadBytes) }
		attributes = append(attributes, doc.Attributes...)
		for _, attr := range doc.Attributes { if _, ok := attr.(*tg.DocumentAttributeVideo); ok { kind = "video"; break } }
	}

	if strings.TrimSpace(tempDir) == "" { tempDir = os.TempDir() }
	if err := os.MkdirAll(tempDir, 0700); err != nil { return nil, fmt.Errorf("create temp directory: %w", err) }
	expectedSize := int64(0)
	if doc, ok := elem.Document(); ok { expectedSize = doc.Size }
	if expectedSize > MaxDownloadBytes { return nil, fmt.Errorf("media is larger than the configured %d-byte limit", MaxDownloadBytes) }
	if err := checkDiskSpace(tempDir, expectedSize); err != nil { return nil, err }
	f, err := os.CreateTemp(tempDir, "theogbot-*")
	if err != nil { return nil, fmt.Errorf("create temp file: %w", err) }
	path := f.Name()
	defer func(){ if closeErr := f.Close(); err == nil && closeErr != nil { err = closeErr }; if err != nil { _ = os.Remove(path) } }()

	writer := &progressWriterAt{file:f,total:expectedSize,callback:progress}
	_, err = downloader.NewDownloader().Download(api, file.Location).WithThreads(4).Parallel(ctx, writer)
	if err != nil { return nil, fmt.Errorf("download %q: %w", file.Name, err) }
	if err = f.Sync(); err != nil { return nil, fmt.Errorf("sync downloaded file: %w", err) }
	info, err := os.Stat(path)
	if err != nil { return nil, err }
	if info.Size() == 0 { return nil, errors.New("Telegram returned an empty media file") }
	if info.Size() > MaxDownloadBytes { return nil, fmt.Errorf("download exceeded the configured %d-byte limit", MaxDownloadBytes) }

	name := safeFilename(file.Name)
	return &Downloaded{Path:path, Name:name, MIME:file.MIMEType, Size:info.Size(), Kind:kind, Caption:msg.Message, Entities:msg.Entities, Attributes:attributes, GroupedID:groupedID}, nil
}

func safeFilename(raw string) string {
	name := strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	name = path.Base(name)
	name = strings.Map(func(r rune) rune { if r == 0 || r < 32 || r == 127 { return -1 }; return r }, name)
	if name == "" || name == "." || name == ".." || name == "/" { return "telegram-file" }
	return filepath.Base(name)
}

func (d *Downloaded) Cleanup() error {
	if d == nil || d.Path == "" { return nil }
	return os.Remove(d.Path)
}
