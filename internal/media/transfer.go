package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"path"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

var ErrProtectedMessage = errors.New("Telegram marks this message as protected (noforwards); download was not attempted")

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
func DownloadMessage(ctx context.Context, api *tg.Client, elem messages.Elem, tempDir string) (_ *Downloaded, err error) {
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
	f, err := os.CreateTemp(tempDir, "theogbot-*")
	if err != nil { return nil, fmt.Errorf("create temp file: %w", err) }
	path := f.Name()
	if err := f.Close(); err != nil { _ = os.Remove(path); return nil, err }
	defer func(){ if err != nil { _ = os.Remove(path) } }()

	_, err = downloader.NewDownloader().Download(api, file.Location).WithThreads(4).ToPath(ctx, path)
	if err != nil { return nil, fmt.Errorf("download %q: %w", file.Name, err) }
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
