// Команда gameserver — учебный игровой сервер модуля 15: подмножество
// протокола Minecraft Java Edition 1.21 / 1.21.1 (протокол 767) и
// WebSocket-шлюз для браузерного клиента на одном игровом цикле.
//
// Конфигурация — переменные окружения GAME_* (см. README). Сборка частей
// сервера — в internal/app.Run; при SIGINT/SIGTERM сервер отключает
// игроков, сохраняет мир и выходит.
package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"gotutor/labs/gameserver/internal/app"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "gameserver:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) > 0 {
		fmt.Fprintln(stdout, "usage: gameserver (configured by GAME_* environment variables, see README)")
		if args[0] == "-h" || args[0] == "-help" || args[0] == "--help" {
			return nil
		}
		return fmt.Errorf("unexpected arguments: %q", args)
	}
	cfg, err := app.LoadConfig(getenv)
	if err != nil {
		return err
	}
	log := slog.New(slog.NewJSONHandler(stdout, &slog.HandlerOptions{Level: cfg.LogLevel})).With("service", "gameserver")
	return app.Run(ctx, cfg, log, nil)
}
