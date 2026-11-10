package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aniketnotcoder/theogbot/internal/account"
	"github.com/aniketnotcoder/theogbot/internal/bot"
	"github.com/aniketnotcoder/theogbot/internal/config"
	"github.com/aniketnotcoder/theogbot/internal/storage"
	"github.com/aniketnotcoder/theogbot/internal/storageprefs"
	"github.com/aniketnotcoder/theogbot/internal/wistia"
)

func main() {
	cfg, err := config.Load()
	if err != nil { slog.Error("configuration error", "error", err); os.Exit(1) }
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	accounts := account.NewManager(cfg.APIID, cfg.APIHash)
	app := bot.New(cfg.BotToken, logger, accounts)
	// Wistia is an optional side feature. Missing Wistia variables never prevent
	// the existing Telegram-to-Telegram bot from starting.
	if os.Getenv("WISTIA_API_TOKEN") != "" || os.Getenv("WISTIA_PROJECT_ID") != "" {
		wistiaCfg, cfgErr := wistia.ConfigFromEnv()
		if cfgErr != nil {
			logger.Warn("Wistia integration disabled because its configuration is incomplete", "error", cfgErr)
		} else if wistiaClient, clientErr := wistia.NewClient(wistiaCfg); clientErr != nil {
			logger.Warn("Wistia integration disabled", "error", clientErr)
		} else {
			app.SetWistia(wistiaClient)
			logger.Info("Wistia direct-upload integration enabled")
		}
	}
	app.SetTempDir(cfg.TempDir)
	prefs, err := storageprefs.Open(cfg.StorageDBPath)
	if err != nil { logger.Error("storage preferences database error", "error", err); os.Exit(1) }
	defer prefs.Close()
	if err := app.SetStoragePrefs(prefs); err != nil { logger.Error("load storage preferences", "error", err); os.Exit(1) }

	// Storage upload uses an independent bot-authenticated MTProto client.
	// Start it even without a shared target: /setstorage assigns destinations per user.
	{
		store, err := storage.New(cfg.APIID, cfg.APIHash, cfg.BotToken, cfg.StorageTarget)
		if err != nil { logger.Error("storage configuration error", "error", err); os.Exit(1) }
		app.SetStorage(store)
		go func(){ if err := store.Run(ctx); err != nil && ctx.Err() == nil { logger.Error("storage client stopped", "error", err) } }()
	}
	if err := app.Run(ctx); err != nil && ctx.Err() == nil { logger.Error("bot stopped", "error", err); os.Exit(1) }
}
