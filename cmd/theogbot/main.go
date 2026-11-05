package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/aniketnotcoder/theogbot/internal/bot"
	"github.com/aniketnotcoder/theogbot/internal/config"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("configuration error", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	app := bot.New(cfg.BotToken, logger)
	if err := app.Run(ctx); err != nil && ctx.Err() == nil {
		logger.Error("bot stopped", "error", err)
		os.Exit(1)
	}
}
