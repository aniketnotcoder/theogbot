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
	if result.PlayerURL != "" || result.EmbedURL != "" { t.Fatalf("upload response must not manufacture player/embed URLs: %+v", result) }
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

func TestExactSizeWriterRejectsOverflow(t *testing.T) {
	var output strings.Builder
	writer := &exactSizeWriter{w: &output, remaining: 5}
	if _, err := writer.Write([]byte("123456")); err == nil {
		t.Fatal("expected oversized write to fail")
	}
	if output.Len() != 0 {
		t.Fatalf("oversized write wrote %d bytes", output.Len())
	}
	n, err := writer.Write([]byte("12345"))
	if err != nil || n != 5 {
		t.Fatalf("expected exact-size write, n=%d err=%v", n, err)
	}
	if writer.written != 5 || writer.remaining != 0 {
		t.Fatalf("unexpected writer accounting: %+v", writer)
	}
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
		_, _ = io.WriteString(w, `{"data":{"id":"abc123","attributes":{"name":"Demo","status":"ready","assets":[{"type":"OriginalFile","url":"https://embed-ssl.wistia.com/deliveries/original.bin","contentType":"video/mp4"},{"type":"HdMp4VideoFile","url":"https://embed-ssl.wistia.com/deliveries/89a55ffaebc2e8af0f996042f24191c99e8afcc1.bin","contentType":"video/mp4"}]}}}`)
	}))
	defer server.Close()
	client, err := NewClient(Config{UploadToken:"upload",DataAPIToken:"read-test-token",FolderID:"folder",APIEndpoint:server.URL,HTTPClient:server.Client()})
	if err != nil { t.Fatal(err) }
	info, err := client.GetMedia(context.Background(), "abc123")
	if err != nil { t.Fatal(err) }
	if info.Status != "ready" || info.Name != "Demo" { t.Fatalf("unexpected media info: %+v", info) }
	if info.DeliveryURL != "https://embed-ssl.wistia.com/deliveries/89a55ffaebc2e8af0f996042f24191c99e8afcc1.bin" { t.Fatalf("delivery URL = %q", info.DeliveryURL) }
	if info.MP4URL != "https://embed-ssl.wistia.com/deliveries/89a55ffaebc2e8af0f996042f24191c99e8afcc1/Demo.mp4" { t.Fatalf("MP4 URL = %q", info.MP4URL) }
	if info.AssetType != "HdMp4VideoFile" { t.Fatalf("asset type = %q", info.AssetType) }
}


func TestMP4AssetURLPreservesQueryAndUsesWistiaPathConvention(t *testing.T) {
	got := mp4AssetURL("https://embed-ssl.wistia.com/deliveries/hash.bin?foo=bar", "Lesson 01.mov")
	want := "https://embed-ssl.wistia.com/deliveries/hash/Lesson%2001.mp4?foo=bar"
	if got != want { t.Fatalf("mp4 URL = %q, want %q", got, want) }
}

func TestFindMP4AssetURLFallsBackToOriginalOnlyWhenNoEncodedMP4Exists(t *testing.T) {
	raw := map[string]any{"data":map[string]any{"attributes":map[string]any{"assets":[]any{
		map[string]any{"type":"OriginalFile","url":"https://embed-ssl.wistia.com/deliveries/original.bin","contentType":"video/mp4"},
		map[string]any{"type":"StillImageFile","url":"https://embed-ssl.wistia.com/deliveries/still.jpg","contentType":"image/jpeg"},
	}}}}
	url, kind := findMP4AssetURL(raw)
	if url != "https://embed-ssl.wistia.com/deliveries/original.bin" || kind != "OriginalFile" {
		t.Fatalf("fallback asset = %q (%q)", url, kind)
	}
}
