package wistia

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

type Source func(context.Context, io.Writer) (int64, error)

type countWriter struct{ n int64 }
func (w *countWriter) Write(p []byte) (int, error) { w.n += int64(len(p)); return len(p), nil }

func (c *Client) UploadPath(ctx context.Context, filePath, displayName string, progress ProgressFunc) (UploadResult, error) {
	f, err := os.Open(filePath)
	if err != nil { return UploadResult{}, fmt.Errorf("open upload file: %w", err) }
	defer f.Close()
	info, err := f.Stat()
	if err != nil { return UploadResult{}, fmt.Errorf("stat upload file: %w", err) }
	if !info.Mode().IsRegular() || info.Size() <= 0 { return UploadResult{}, fmt.Errorf("upload source must be a non-empty regular file") }
	filename := safeFilename(filepath.Base(filePath))
	if displayName == "" { displayName = filename }
	source := func(ctx context.Context, dst io.Writer) (int64, error) {
		return copyWithProgress(ctx, dst, f, info.Size(), progress)
	}
	return c.UploadSource(ctx, filename, displayName, info.Size(), source, progress)
}

// UploadSource streams a known-size source into Wistia without buffering the media.
// Source is called once in a producer goroutine and must return the exact number of bytes written.
func (c *Client) UploadSource(ctx context.Context, filename, displayName string, size int64, source Source, progress ProgressFunc) (UploadResult, error) {
	if c == nil { return UploadResult{}, fmt.Errorf("Wistia client is nil") }
	if size <= 0 { return UploadResult{}, fmt.Errorf("video size must be greater than zero") }
	if source == nil { return UploadResult{}, fmt.Errorf("upload source is required") }
	filename = safeFilename(filename)
	if displayName == "" { displayName = filename }

	boundaryBytes := make([]byte, 24)
	if _, err := rand.Read(boundaryBytes); err != nil { return UploadResult{}, fmt.Errorf("generate multipart boundary: %w", err) }
	boundary := "theogbot-" + hex.EncodeToString(boundaryBytes)
	overhead, err := multipartOverhead(boundary, c.folderID, displayName, filename)
	if err != nil { return UploadResult{}, err }

	uploadCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	reader, writer := io.Pipe()
	producerResult := make(chan error, 1)
	go func() {
		producerErr := writeMultipart(uploadCtx, writer, boundary, c.folderID, displayName, filename, size, source)
		_ = writer.CloseWithError(producerErr)
		producerResult <- producerErr
	}()

	req, err := http.NewRequestWithContext(uploadCtx, http.MethodPost, c.uploadURL, reader)
	if err != nil {
		_ = reader.CloseWithError(err)
		cancel()
		<-producerResult
		return UploadResult{}, fmt.Errorf("create Wistia upload request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.uploadToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
	req.ContentLength = overhead + size

	resp, requestErr := c.http.Do(req)
	if requestErr != nil {
		_ = reader.CloseWithError(requestErr)
		cancel()
		<-producerResult
		return UploadResult{}, fmt.Errorf("Wistia upload request failed: %w", requestErr)
	}
	defer resp.Body.Close()
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if readErr != nil {
		_ = reader.CloseWithError(readErr)
		cancel()
		<-producerResult
		return UploadResult{}, fmt.Errorf("read Wistia upload response: %w", readErr)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		rejection := fmt.Errorf("%w (HTTP %d): %s", ErrUploadRejected, resp.StatusCode, strings.TrimSpace(string(body)))
		_ = reader.CloseWithError(rejection)
		cancel()
		<-producerResult
		return UploadResult{}, rejection
	}
	if producerErr := <-producerResult; producerErr != nil {
		return UploadResult{}, fmt.Errorf("stream source to Wistia: %w", producerErr)
	}

	result, err := parseUploadResult(body)
	if err != nil {
		return UploadResult{}, err
	}
	result.FolderID = c.folderID
	result.PlayerURL = "https://fast.wistia.net/embed/iframe/" + result.HashedID
	result.EmbedURL = result.PlayerURL
	return result, nil
}

func multipartOverhead(boundary, folderID, displayName, filename string) (int64, error) {
	counter := &countWriter{}
	mw := multipart.NewWriter(counter)
	if err := mw.SetBoundary(boundary); err != nil { return 0, err }
	if err := writeFields(mw, folderID, displayName); err != nil { return 0, err }
	if _, err := mw.CreateFormFile("file", filename); err != nil { return 0, err }
	if err := mw.Close(); err != nil { return 0, err }
	return counter.n, nil
}

func writeFields(mw *multipart.Writer, folderID, displayName string) error {
	for key, value := range map[string]string{"project_id": folderID, "name": displayName} {
		part, err := mw.CreateFormField(key)
		if err != nil { return err }
		if _, err := io.WriteString(part, value); err != nil { return err }
	}
	return nil
}

func writeMultipart(ctx context.Context, dst *io.PipeWriter, boundary, folderID, displayName, filename string, expected int64, source Source) error {
	mw := multipart.NewWriter(dst)
	if err := mw.SetBoundary(boundary); err != nil { return err }
	if err := writeFields(mw, folderID, displayName); err != nil { return err }
	part, err := mw.CreateFormFile("file", filename)
	if err != nil { return err }
	written, sourceErr := source(ctx, part)
	if sourceErr != nil { return sourceErr }
	if written != expected { return fmt.Errorf("source size mismatch: declared %d bytes, wrote %d", expected, written) }
	return mw.Close()
}

func parseUploadResult(body []byte) (UploadResult, error) {
	var result UploadResult
	if err := json.Unmarshal(body, &result); err != nil { return result, fmt.Errorf("decode Wistia upload response: %w", err) }
	if result.HashedID == "" {
		var wrapped struct { Data struct { ID string `json:"id"`; Attributes struct { HashedID string `json:"hashed_id"`; Name string `json:"name"`; Status string `json:"status"` } `json:"attributes"` } `json:"data"` }
		if err := json.Unmarshal(body, &wrapped); err == nil {
			result.HashedID = wrapped.Data.Attributes.HashedID
			if result.HashedID == "" { result.HashedID = wrapped.Data.ID }
			if result.Name == "" { result.Name = wrapped.Data.Attributes.Name }
			if result.Status == "" { result.Status = wrapped.Data.Attributes.Status }
		}
	}
	if strings.TrimSpace(result.HashedID) == "" { return result, fmt.Errorf("Wistia accepted the request but returned no media hashed_id") }
	return result, nil
}

func safeFilename(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = path.Base(value)
	value = strings.Map(func(r rune) rune { if r == 0 || r < 32 || r == 127 { return -1 }; return r }, value)
	if value == "" || value == "." || value == "/" { return "telegram-video" }
	return filepath.Base(value)
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, total int64, progress ProgressFunc) (int64, error) {
	buf := make([]byte, 256*1024)
	var copied int64
	for {
		if err := ctx.Err(); err != nil { return copied, err }
		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := dst.Write(buf[:n])
			copied += int64(wn)
			if progress != nil && wn > 0 { progress(copied, total) }
			if writeErr != nil { return copied, writeErr }
			if wn != n { return copied, io.ErrShortWrite }
		}
		if readErr == io.EOF { return copied, nil }
		if readErr != nil { return copied, readErr }
	}
}

type progressWriter struct {
	w io.Writer
	total int64
	n int64
	fn ProgressFunc
	mu sync.Mutex
}
func (w *progressWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.mu.Lock()
	w.n += int64(n)
	current := w.n
	w.mu.Unlock()
	if n > 0 && w.fn != nil { w.fn(current, w.total) }
	return n, err
}
