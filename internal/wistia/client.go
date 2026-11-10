package wistia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	if info.Status == "" {
		return info, fmt.Errorf("Wistia media response did not include a status")
	}
	return info, nil
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
			return info, nil
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
