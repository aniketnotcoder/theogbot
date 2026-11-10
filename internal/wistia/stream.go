package wistia

import (
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"

	"github.com/gotd/td/telegram/downloader"
	"github.com/gotd/td/telegram/query/messages"
	"github.com/gotd/td/tg"
)

// UploadTelegramMessage streams one accessible Telegram video document to Wistia.
// gotd Stream is deliberately used here: it writes to io.Writer but is sequential.
// The existing parallel disk-backed Telegram transfer implementation is not changed.
func (c *Client) UploadTelegramMessage(ctx context.Context, api *tg.Client, elem messages.Elem, progress ProgressFunc) (UploadResult, error) {
	if api == nil { return UploadResult{}, fmt.Errorf("source Telegram MTProto API is required") }
	msg, ok := elem.Msg.(*tg.Message)
	if !ok { return UploadResult{}, fmt.Errorf("unsupported Telegram message type %T", elem.Msg) }
	if msg.Noforwards { return UploadResult{}, ErrProtectedMedia }
	doc, ok := elem.Document()
	if !ok || doc == nil { return UploadResult{}, ErrInvalidMedia }
	file, ok := elem.File()
	if !ok { return UploadResult{}, ErrInvalidMedia }
	isVideo := strings.HasPrefix(strings.ToLower(doc.MimeType), "video/")
	for _, attr := range doc.Attributes {
		if _, ok := attr.(*tg.DocumentAttributeVideo); ok { isVideo = true; break }
	}
	if !isVideo { return UploadResult{}, ErrInvalidMedia }
	if doc.Size <= 0 { return UploadResult{}, fmt.Errorf("Telegram document has an invalid size: %d", doc.Size) }

	filename := safeFilename(file.Name)
	displayName := strings.TrimSpace(msg.Message)
	if displayName == "" { displayName = filename }
	if strings.TrimSpace(file.MIMEType) == "" {
		if guessed := mime.TypeByExtension(filepath.Ext(filename)); guessed != "" { _ = guessed }
	}
	source := func(ctx context.Context, dst io.Writer) (int64, error) {
		pw := &progressWriter{w: dst, total: doc.Size, fn: progress}
		_, err := downloader.NewDownloader().Download(api, file.Location).Stream(ctx, pw)
		if err != nil { return pw.n, fmt.Errorf("download Telegram video stream: %w", err) }
		return pw.n, nil
	}
	return c.UploadSource(ctx, filename, displayName, doc.Size, source, nil)
}
