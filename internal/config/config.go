package config

import (
 "errors"
 "os"
 "strconv"
)

type Config struct { BotToken string; APIID int; APIHash string }

func Load() (Config, error) {
 cfg := Config{BotToken: os.Getenv("BOT_TOKEN"), APIHash: os.Getenv("API_HASH")}
 if cfg.BotToken == "" { return Config{}, errors.New("BOT_TOKEN is required") }
 rawID := os.Getenv("API_ID")
 if rawID == "" || cfg.APIHash == "" { return Config{}, errors.New("API_ID and API_HASH are required for MTProto login") }
 id, err := strconv.Atoi(rawID)
 if err != nil || id <= 0 { return Config{}, errors.New("API_ID must be a positive integer") }
 cfg.APIID = id
 return cfg, nil
}
