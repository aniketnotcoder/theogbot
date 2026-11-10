package wistia

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

// StageProgressFunc reports progress for a named phase of a disk-backed transfer.
type StageProgressFunc func(stage string, current, total int64)

type downloadWriterAt struct {
	file *os.File
	total int64
	written atomic.Int64
	progress StageProgressFunc
	progressMu sync.Mutex
}

func (w *downloadWriterAt) WriteAt(p []byte, off int64) (int, error) {
	if off < 0 || off > w.total || int64(len(p)) > w.total-off {
		return 0, fmt.Errorf("download chunk exceeds declared Telegram video size")
	}
	n, err := w.file.WriteAt(p, off)
	current := w.written.Add(int64(n))
	if n > 0 && w.progress != nil {
		w.progressMu.Lock()
		w.progress("Downloading", current, w.total)
		w.progressMu.Unlock()
	}
	return n, err
}

// UploadTelegramMessageFromFile downloads a Telegram video with gotd's parallel
// WriterAt engine, uploads the completed file to Wistia, and removes it only
// after a confirmed successful upload. On upload failure, tempPath is returned
// so the caller can reconcile the outcome or retry without downloading again.
func (c *Client) UploadTelegramMessageFromFile(
	ctx context.Context,
	api *tg.Client,
	elem messages.Elem,
	tempDir string,
	progress StageProgressFunc,
) (result UploadResult, tempPath string, err error) {
	if c == nil {
		return UploadResult{}, "", fmt.Errorf("Wistia client is nil")
	}
	if api == nil {
		return UploadResult{}, "", fmt.Errorf("source Telegram MTProto API is required")
	}
	msg, ok := elem.Msg.(*tg.Message)
	if !ok {
		return UploadResult{}, "", fmt.Errorf("unsupported Telegram message type %T", elem.Msg)
	}
	if msg.Noforwards {
		return UploadResult{}, "", ErrProtectedMedia
	}
	doc, ok := elem.Document()
	if !ok || doc == nil {
		return UploadResult{}, "", ErrInvalidMedia
	}
	file, ok := elem.File()
	if !ok {
		return UploadResult{}, "", ErrInvalidMedia
	}
	isVideo := strings.HasPrefix(strings.ToLower(doc.MimeType), "video/")
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeVideo); ok {
			isVideo = true
			break
		}
	}
	if !isVideo {
		return UploadResult{}, "", ErrInvalidMedia
	}
	if doc.Size <= 0 {
		return UploadResult{}, "", fmt.Errorf("Telegram document has an invalid size: %d", doc.Size)
	}
	filename := safeFilename(file.Name)
	displayName := strings.TrimSpace(msg.Message)
	if displayName == "" {
		displayName = filename
	}
	if strings.TrimSpace(tempDir) == "" {
		tempDir = os.TempDir()
	}
	if err := os.MkdirAll(tempDir, 0700); err != nil {
		return UploadResult{}, "", fmt.Errorf("create Wistia temporary directory: %w", err)
	}
	ext := filepath.Ext(filename)
	f, err := os.CreateTemp(tempDir, "theogbot-wistia-*"+ext)
	if err != nil {
		return UploadResult{}, "", fmt.Errorf("create Wistia temporary file: %w", err)
	}
	tempPath = f.Name()
	keepFile := false
	fileClosed := false
	defer func() {
		if !fileClosed {
			if closeErr := f.Close(); err == nil && closeErr != nil { err = closeErr }
		}
		if !keepFile {
			if removeErr := os.Remove(tempPath); err == nil && removeErr != nil && !os.IsNotExist(removeErr) {
				err = fmt.Errorf("remove Wistia temporary file: %w", removeErr)
			}
		}
	}()

	writer := &downloadWriterAt{file: f, total: doc.Size, progress: progress}
	_, err = downloader.NewDownloader().Download(api, file.Location).
		WithThreads(c.downloadThreads).
		Parallel(ctx, writer)
	if err != nil {
		return UploadResult{}, "", fmt.Errorf("download Telegram video to temporary file: %w", err)
	}
	if err = f.Sync(); err != nil {
		return UploadResult{}, "", fmt.Errorf("sync Wistia temporary video: %w", err)
	}
	if err = f.Close(); err != nil {
		return UploadResult{}, "", fmt.Errorf("close Wistia temporary video: %w", err)
	}
	fileClosed = true
	info, err := os.Stat(tempPath)
	if err != nil {
		return UploadResult{}, "", fmt.Errorf("stat Wistia temporary video: %w", err)
	}
	if info.Size() != doc.Size {
		return UploadResult{}, "", fmt.Errorf("download size mismatch: Telegram declared %d bytes, disk contains %d", doc.Size, info.Size())
	}
	uploadProgress := ProgressFunc(nil)
	if progress != nil {
		uploadProgress = func(current, total int64) { progress("Uploading", current, total) }
	}
	result, err = c.UploadFile(ctx, tempPath, filename, displayName, uploadProgress)
	if err != nil {
		keepFile = true
		return UploadResult{}, tempPath, err
	}
	if removeErr := os.Remove(tempPath); removeErr != nil && !os.IsNotExist(removeErr) {
		keepFile = true
		return result, tempPath, fmt.Errorf("upload succeeded but temporary file cleanup failed: %w", removeErr)
	}
	keepFile = true // already removed; prevent the deferred cleanup from masking the result.
	return result, "", nil
}
