package config

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	BotToken string
	APIID int
	APIHash string
	StorageTarget string
	TempDir string
	StorageDBPath string
}

func Load() (Config, error) {
	cfg := Config{
		BotToken: strings.TrimSpace(os.Getenv("BOT_TOKEN")),
		APIHash: strings.TrimSpace(os.Getenv("API_HASH")),
		StorageTarget: strings.TrimSpace(os.Getenv("STORAGE_TARGET")),
		TempDir: strings.TrimSpace(os.Getenv("TEMP_DIR")),
		StorageDBPath: strings.TrimSpace(os.Getenv("STORAGE_DB_PATH")),
	}
	if cfg.BotToken == "" { return Config{}, errors.New("BOT_TOKEN is required") }
	rawID := os.Getenv("API_ID")
	if rawID == "" || cfg.APIHash == "" { return Config{}, errors.New("API_ID and API_HASH are required for MTProto login") }
	id, err := strconv.Atoi(rawID)
	if err != nil || id <= 0 { return Config{}, errors.New("API_ID must be a positive integer") }
	cfg.APIID = id
	if cfg.TempDir == "" { cfg.TempDir = os.TempDir() }
	if cfg.StorageDBPath == "" { cfg.StorageDBPath = "data/theogbot.db" }
	return cfg, nil
}
