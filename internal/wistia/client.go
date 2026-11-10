package wistia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type Client struct {
	uploadToken string
	dataToken   string
	folderID    string
	uploadURL   string
	apiURL      string
	apiVersion  string
	http        *http.Client
	pollEvery   time.Duration
	downloadThreads int
}

func NewClient(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.UploadToken) == "" {
		return nil, ErrUploadTokenRequired
	}
	if strings.TrimSpace(cfg.FolderID) == "" {
		return nil, ErrFolderRequired
	}
	if cfg.UploadEndpoint == "" {
		cfg.UploadEndpoint = defaultUploadEndpoint
	}
	if cfg.APIEndpoint == "" {
		cfg.APIEndpoint = defaultAPIEndpoint
	}
	if cfg.APIVersion == "" {
		cfg.APIVersion = defaultAPIVersion
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{
			Transport: &http.Transport{
				Proxy: http.ProxyFromEnvironment,
				MaxIdleConns: 16,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout: 90 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
				ExpectContinueTimeout: time.Second,
				ResponseHeaderTimeout: 60 * time.Second,
			},
		}
	}
	if cfg.DownloadThreads < 1 || cfg.DownloadThreads > 32 {
		cfg.DownloadThreads = 8
	}
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 20 * time.Second
	}
	return &Client{
		uploadToken: strings.TrimSpace(cfg.UploadToken),
		dataToken: strings.TrimSpace(cfg.DataAPIToken),
		folderID: strings.TrimSpace(cfg.FolderID),
		uploadURL: strings.TrimSpace(cfg.UploadEndpoint),
		apiURL: strings.TrimRight(strings.TrimSpace(cfg.APIEndpoint), "/"),
		apiVersion: strings.TrimSpace(cfg.APIVersion),
		http: cfg.HTTPClient,
		pollEvery: cfg.PollInterval,
		downloadThreads: cfg.DownloadThreads,
	}, nil
}

// HasDataAPI reports whether media status lookups are available.
func (c *Client) HasDataAPI() bool { return c != nil && strings.TrimSpace(c.dataToken) != "" }

func (c *Client) doDataGET(ctx context.Context, endpoint string) ([]byte, error) {
	if c == nil || c.dataToken == "" {
		return nil, ErrDataTokenRequired
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create Wistia Data API request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.dataToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Wistia-API-Version", c.apiVersion)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Wistia Data API request: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		return nil, fmt.Errorf("read Wistia Data API response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Wistia Data API returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *Client) GetMedia(ctx context.Context, hashedID string) (MediaInfo, error) {
	hashedID = strings.TrimSpace(hashedID)
	if hashedID == "" {
		return MediaInfo{}, fmt.Errorf("Wistia media hashed ID is required")
	}
	endpoint := c.apiURL + "/modern/medias/" + url.PathEscape(hashedID)
	body, err := c.doDataGET(ctx, endpoint)
	if err != nil {
		return MediaInfo{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return MediaInfo{}, fmt.Errorf("decode Wistia media response: %w", err)
	}
	info := MediaInfo{HashedID: hashedID}
	readString := func(m map[string]any, key string) string {
		if v, ok := m[key].(string); ok { return v }
		return ""
	}
	info.Name = readString(raw, "name")
	info.Status = readString(raw, "status")
	if data, ok := raw["data"].(map[string]any); ok {
		if info.Name == "" { info.Name = readString(data, "name") }
		if info.Status == "" { info.Status = readString(data, "status") }
		if attrs, ok := data["attributes"].(map[string]any); ok {
			if info.Name == "" { info.Name = readString(attrs, "name") }
			if info.Status == "" { info.Status = readString(attrs, "status") }
		}
	}
	if assetURL, assetType := findMP4AssetURL(raw); assetURL != "" {
		info.DeliveryURL = assetURL
		info.MP4URL = mp4AssetURL(assetURL, info.Name)
		info.AssetType = assetType
	}
	if info.Status == "" {
		return info, fmt.Errorf("Wistia media response did not include a status")
	}
	return info, nil
}

func findMP4AssetURL(value any) (string, string) {
	bestURL, bestType, bestRank := "", "", 0
	var walk func(any)
	walk = func(node any) {
		switch item := node.(type) {
		case map[string]any:
			if rawAssets, ok := item["assets"].([]any); ok {
				for _, rawAsset := range rawAssets {
					asset, ok := rawAsset.(map[string]any)
					if !ok { continue }
					assetURL, _ := asset["url"].(string)
					assetType, _ := asset["type"].(string)
					contentType, _ := asset["contentType"].(string)
					if assetURL == "" || (!strings.HasPrefix(strings.ToLower(contentType), "video/") && !strings.HasSuffix(strings.ToLower(assetURL), ".bin") && !strings.HasSuffix(strings.ToLower(assetURL), ".mp4")) { continue }
					rank := 1
					switch assetType {
					case "HdMp4VideoFile": rank = 5
					case "Mp4VideoFile": rank = 4
					case "MdMp4VideoFile": rank = 3
					case "IPhoneVideoFile": rank = 2
					case "OriginalFile": rank = 0
					}
					if rank > bestRank { bestURL, bestType, bestRank = assetURL, assetType, rank }
				}
			}
			for _, child := range item { walk(child) }
		case []any:
			for _, child := range item { walk(child) }
		}
	}
	walk(value)
	return bestURL, bestType
}

// mp4AssetURL applies Wistia's documented extension convention to an asset URL.
// The .bin and named .mp4 URLs address the same underlying asset.
func mp4AssetURL(assetURL, name string) string {
	u, err := url.Parse(assetURL)
	if err != nil { return "" }
	if strings.HasSuffix(strings.ToLower(u.Path), ".mp4") { return u.String() }
	if !strings.HasSuffix(strings.ToLower(u.Path), ".bin") { return "" }
	filename := safeFilename(name)
	ext := strings.ToLower(path.Ext(filename))
	if ext != ".mp4" { filename = strings.TrimSuffix(filename, path.Ext(filename)) + ".mp4" }
	u.Path = strings.TrimSuffix(u.Path, ".bin") + "/" + filename
	return u.String()
}

func (c *Client) WaitUntilReady(ctx context.Context, hashedID string) (MediaInfo, error) {
	ticker := time.NewTicker(c.pollEvery)
	defer ticker.Stop()
	for {
		info, err := c.GetMedia(ctx, hashedID)
		if err != nil {
			return MediaInfo{}, err
		}
		switch strings.ToLower(info.Status) {
		case "ready":
			if info.DeliveryURL != "" && info.MP4URL != "" { return info, nil }
		case "failed", "error":
			return info, fmt.Errorf("%w: %s", ErrMediaProcessingFailed, info.Status)
		}
		select {
		case <-ctx.Done():
			return MediaInfo{}, ctx.Err()
		case <-ticker.C:
		}
	}
}
