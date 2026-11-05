package config

import (
	"errors"
	"os"
	"strconv"
)

type Config struct {
	BotToken string
	APIID    int
	APIHash  string
}

func Load() (Config, error) {
	cfg := Config{BotToken: os.Getenv("BOT_TOKEN"), APIHash: os.Getenv("API_HASH")}
	if cfg.BotToken == "" {
		return Config{}, errors.New("BOT_TOKEN is required")
	}
	if raw := os.Getenv("API_ID"); raw != "" {
		id, err := strconv.Atoi(raw)
		if err != nil || id <= 0 {
			return Config{}, errors.New("API_ID must be a positive integer")
		}
		cfg.APIID = id
	}
	return cfg, nil
}
