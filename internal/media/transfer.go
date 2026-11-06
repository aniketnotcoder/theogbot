package media

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"path/filepath"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

var ErrProtectedMessage = errors.New("Telegram marks this message as protected (noforwards); download was not attempted")

type Downloaded struct { Path, Name, MIME string; Size int64 }

// DownloadMessage downloads a photo/document attachment from an accessible,
// non-protected message into a temporary file. Telegram remains authoritative
// about whether a media location can be retrieved.
func DownloadMessage(ctx context.Context, api *tg.Client, elem messages.Elem, tempDir string) (_ *Downloaded, err error) {
	if api == nil { return nil, errors.New("source MTProto API is required") }
	msg, ok := elem.Msg.(*tg.Message)
	if !ok { return nil, fmt.Errorf("unsupported message type %T", elem.Msg) }
	if msg.Noforwards { return nil, ErrProtectedMessage }
	file, ok := elem.File()
	if !ok { return nil, errors.New("message has no photo or document attachment") }
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
	name := filepath.Base(strings.TrimSpace(file.Name))
	if name == "." || name == string(filepath.Separator) || name == "" { name = "telegram-file" }
	return &Downloaded{Path:path, Name:name, MIME:file.MIMEType, Size:info.Size()}, nil
}

func (d *Downloaded) Cleanup() error {
	if d == nil || d.Path == "" { return nil }
	return os.Remove(d.Path)
}
