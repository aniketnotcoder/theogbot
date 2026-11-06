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
)

func main() {
	cfg, err := config.Load()
	if err != nil { slog.Error("configuration error", "error", err); os.Exit(1) }
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	accounts := account.NewManager(cfg.APIID, cfg.APIHash)
	app := bot.New(cfg.BotToken, logger, accounts)
	app.SetTempDir(cfg.TempDir)

	// Storage upload uses an independent bot-authenticated MTProto client.
	// It is optional until STORAGE_TARGET is configured.
	if cfg.StorageTarget != "" {
		store, err := storage.New(cfg.APIID, cfg.APIHash, cfg.BotToken, cfg.StorageTarget)
		if err != nil { logger.Error("storage configuration error", "error", err); os.Exit(1) }
		app.SetStorage(store)
		go func(){ if err := store.Run(ctx); err != nil && ctx.Err() == nil { logger.Error("storage client stopped", "error", err) } }()
	}
	if err := app.Run(ctx); err != nil && ctx.Err() == nil { logger.Error("bot stopped", "error", err); os.Exit(1) }
}
