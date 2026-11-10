package wistia

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testClient(t *testing.T, handler http.Handler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := NewClient(Config{
		UploadToken: "upload-test-token",
		DataAPIToken: "read-test-token",
		FolderID: "folder123",
		UploadEndpoint: server.URL,
		APIEndpoint: server.URL,
		HTTPClient: server.Client(),
	})
	if err != nil { t.Fatal(err) }
	return client, server
}

func TestUploadSourceStreamsMultipartWithKnownLength(t *testing.T) {
	payload := []byte("small-video-payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer upload-test-token" { t.Errorf("unexpected authorization header") }
		if r.ContentLength <= int64(len(payload)) { t.Errorf("expected known multipart content length, got %d", r.ContentLength) }
		mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if err != nil || mediaType != "multipart/form-data" { t.Errorf("invalid content type: %q (%v)", mediaType, err); return }
		mr := multipart.NewReader(r.Body, params["boundary"])
		fields := map[string]string{}
		var gotFile []byte
		for {
			part, err := mr.NextPart()
			if err == io.EOF { break }
			if err != nil { t.Errorf("read multipart: %v", err); return }
			data, err := io.ReadAll(part)
			if err != nil { t.Errorf("read part: %v", err); return }
			if part.FormName() == "file" { gotFile = data } else { fields[part.FormName()] = string(data) }
		}
		if fields["project_id"] != "folder123" { t.Errorf("project_id = %q", fields["project_id"]) }
		if fields["name"] != "Demo video" { t.Errorf("name = %q", fields["name"]) }
		if string(gotFile) != string(payload) { t.Errorf("uploaded bytes = %q", gotFile) }
		_ = json.NewEncoder(w).Encode(map[string]string{"hashed_id":"abc123","name":"Demo video","status":"queued"})
	}))
	defer server.Close()
	client, err := NewClient(Config{UploadToken:"upload-test-token",FolderID:"folder123",UploadEndpoint:server.URL,HTTPClient:server.Client()})
	if err != nil { t.Fatal(err) }
	result, err := client.UploadSource(context.Background(), "demo.mp4", "Demo video", int64(len(payload)), func(ctx context.Context, dst io.Writer)(int64,error) {
		n, err := dst.Write(payload)
		return int64(n), err
	})
	if err != nil { t.Fatal(err) }
	if result.HashedID != "abc123" || result.FolderID != "folder123" { t.Fatalf("unexpected result: %+v", result) }
	if result.PlayerURL != "https://fast.wistia.net/embed/iframe/abc123" { t.Fatalf("unexpected player URL: %s", result.PlayerURL) }
}

func TestUploadPath(t *testing.T) {
	payload := []byte("file-upload-payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mediaType, params, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
		if mediaType != "multipart/form-data" { t.Errorf("content type = %s", mediaType) }
		mr := multipart.NewReader(r.Body, params["boundary"])
		for {
			part, err := mr.NextPart()
			if err == io.EOF { break }
			if err != nil { t.Fatal(err) }
			data, _ := io.ReadAll(part)
			if part.FormName() == "file" && string(data) != string(payload) { t.Errorf("file body mismatch") }
		}
		_, _ = io.WriteString(w, `{"hashed_id":"file123","status":"queued"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{UploadToken:"upload-test-token",FolderID:"folder123",UploadEndpoint:server.URL,HTTPClient:server.Client()})
	if err != nil { t.Fatal(err) }
	dir := t.TempDir()
	file := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(file, payload, 0600); err != nil { t.Fatal(err) }
	result, err := client.UploadPath(context.Background(), file, "Clip", nil)
	if err != nil { t.Fatal(err) }
	if result.HashedID != "file123" { t.Fatalf("hashed id = %q", result.HashedID) }
}

func TestUploadSourceRejectsSizeMismatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = io.WriteString(w, `{"hashed_id":"should-not-succeed"}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{UploadToken:"token",FolderID:"folder",UploadEndpoint:server.URL,HTTPClient:server.Client()})
	if err != nil { t.Fatal(err) }
	_, err = client.UploadSource(context.Background(), "x.mp4", "x", 10, func(ctx context.Context, dst io.Writer)(int64,error) {
		n, writeErr := dst.Write([]byte("short"))
		return int64(n), writeErr
	})
	if err == nil || !strings.Contains(err.Error(), "source size mismatch") { t.Fatalf("expected size mismatch, got %v", err) }
}

func TestNewClientRequiresUploadTokenAndFolder(t *testing.T) {
	if _, err := NewClient(Config{FolderID:"folder"}); err != ErrUploadTokenRequired { t.Fatalf("expected upload token error, got %v", err) }
	if _, err := NewClient(Config{UploadToken:"token"}); err != ErrFolderRequired { t.Fatalf("expected folder error, got %v", err) }
}

func TestGetMediaRequiresReadToken(t *testing.T) {
	client, server := testClient(t, http.NotFoundHandler())
	defer server.Close()
	client.dataToken = ""
	if _, err := client.GetMedia(context.Background(), "abc"); err != ErrDataTokenRequired { t.Fatalf("expected read token error, got %v", err) }
}

func TestGetMediaReadsModernAttributes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer read-test-token" { t.Errorf("unexpected data token") }
		if r.Header.Get("X-Wistia-API-Version") != defaultAPIVersion { t.Errorf("unexpected API version") }
		_, _ = io.WriteString(w, `{"data":{"id":"abc123","attributes":{"name":"Demo","status":"ready"}}}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{UploadToken:"upload",DataAPIToken:"read-test-token",FolderID:"folder",APIEndpoint:server.URL,HTTPClient:server.Client()})
	if err != nil { t.Fatal(err) }
	info, err := client.GetMedia(context.Background(), "abc123")
	if err != nil { t.Fatal(err) }
	if info.Status != "ready" || info.Name != "Demo" { t.Fatalf("unexpected media info: %+v", info) }
}
