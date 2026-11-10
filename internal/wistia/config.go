package wistia

import (
	"errors"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultUploadEndpoint = "https://upload.wistia.com/"
	defaultAPIEndpoint    = "https://api.wistia.com"
	defaultAPIVersion     = "2026-09"
)

type Config struct {
	UploadToken   string
	DataAPIToken  string
	FolderID      string
	UploadEndpoint string
	APIEndpoint   string
	APIVersion    string
	HTTPClient    *http.Client
	PollInterval  time.Duration
}

func ConfigFromEnv() (Config, error) {
	cfg := Config{
		UploadToken:    strings.TrimSpace(os.Getenv("WISTIA_API_TOKEN")),
		DataAPIToken:   strings.TrimSpace(os.Getenv("WISTIA_DATA_API_TOKEN")),
		FolderID:       strings.TrimSpace(os.Getenv("WISTIA_PROJECT_ID")),
		UploadEndpoint: defaultUploadEndpoint,
		APIEndpoint:    defaultAPIEndpoint,
		APIVersion:     defaultAPIVersion,
	}
	if cfg.UploadToken == "" {
		return Config{}, errors.New("WISTIA_API_TOKEN is required")
	}
	if cfg.FolderID == "" {
		return Config{}, errors.New("WISTIA_PROJECT_ID is required")
	}
	return cfg, nil
}
