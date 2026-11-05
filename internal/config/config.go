package config

import (
	"errors"
	"os"
)

type Config struct {
	BotToken string
	APIID    string
	APIHash  string
}

func Load() (Config, error) {
	cfg := Config{
		BotToken: os.Getenv("BOT_TOKEN"),
		APIID:    os.Getenv("API_ID"),
		APIHash:  os.Getenv("API_HASH"),
	}
	if cfg.BotToken == "" {
		return Config{}, errors.New("BOT_TOKEN is required")
	}
	return cfg, nil
}
