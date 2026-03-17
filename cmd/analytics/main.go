// Package main is the CLI entrypoint for the analytics service.
// Subcommands: serve, backfill, sync.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/natalie-johanek/trench-analytics/internal/api"
	"github.com/natalie-johanek/trench-analytics/internal/db"
	"github.com/natalie-johanek/trench-analytics/internal/ingestion"
	"github.com/natalie-johanek/trench-analytics/internal/logging"
)

func migrations() db.Migrations {
	return db.Migrations{1: db.Migration001}
}

// Version is set at build time via -ldflags.
var Version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	// Log environment context once at startup (Fly sets these automatically)
	slog.Info("starting",
		"version", Version,
		"region", os.Getenv("FLY_REGION"),
		"instance", os.Getenv("FLY_ALLOC_ID"),
	)

	// Attach environment fields to every wide event
	logging.SetEnvFields(
		"version", Version,
		"region", os.Getenv("FLY_REGION"),
		"instance", os.Getenv("FLY_ALLOC_ID"),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, os.Args[1:]); err != nil {
		slog.Error("command failed", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("usage: analytics <serve|backfill|sync>")
	}
	switch args[0] {
	case "serve":
		return runServe(ctx)
	case "backfill":
		return runBackfill(ctx)
	case "sync":
		return runSync(ctx)
	default:
		return fmt.Errorf("unknown command: %s", args[0])
	}
}

func runServe(ctx context.Context) error {
	store, err := db.Open(ctx, envOrDefault("DUCKDB_PATH", "./trench.db"), migrations())
	if err != nil {
		return err
	}
	defer store.Close()

	client := ingestion.NewClient(
		envOrDefault("TC_BASE_URL", "https://synod.trench-companion.com/wp-json/synod/v1"),
		envIntOrDefault("TC_RATE_LIMIT", 2),
	)
	defer client.Close()

	syncer := ingestion.NewSyncer(client, store)

	apiKey, err := requireEnv("ANALYTICS_API_KEY")
	if err != nil {
		return err
	}
	adminPass, err := requireEnv("ADMIN_PASSWORD")
	if err != nil {
		return err
	}

	server := api.NewServer(
		store, syncer,
		apiKey,
		envOrDefault("ADMIN_USERNAME", "admin"),
		adminPass,
		envIntOrDefault("ANALYTICS_RATE_LIMIT", 60),
	)
	defer server.Close()

	port := envOrDefault("PORT", "8080")
	srv := &http.Server{Addr: ":" + port, Handler: server.Router}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down: stopping HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown error", "err", err)
		}

		slog.Info("shutting down: waiting for background sync to finish")
		server.Sync.Wait()
		slog.Info("shutdown complete")
	}()

	slog.Info("server starting", "port", port)
	if err := srv.ListenAndServe(); err != http.ErrServerClosed {
		return err
	}
	return nil
}

func runBackfill(ctx context.Context) error {
	start, count, err := parseBackfillArgs()
	if err != nil {
		return err
	}

	store, err := db.Open(ctx, envOrDefault("DUCKDB_PATH", "./trench.db"), migrations())
	if err != nil {
		return err
	}
	defer store.Close()

	client := ingestion.NewClient(
		envOrDefault("TC_BASE_URL", "https://synod.trench-companion.com/wp-json/synod/v1"),
		envIntOrDefault("TC_RATE_LIMIT", 2),
	)
	defer client.Close()

	return ingestion.NewSyncer(client, store).Backfill(ctx, start, count)
}

func runSync(ctx context.Context) error {
	store, err := db.Open(ctx, envOrDefault("DUCKDB_PATH", "./trench.db"), migrations())
	if err != nil {
		return err
	}
	defer store.Close()

	client := ingestion.NewClient(
		envOrDefault("TC_BASE_URL", "https://synod.trench-companion.com/wp-json/synod/v1"),
		envIntOrDefault("TC_RATE_LIMIT", 2),
	)
	defer client.Close()

	return ingestion.NewSyncer(client, store).IncrementalSync(ctx, nil)
}

func parseBackfillArgs() (start, count int, err error) {
	args := os.Args[2:]
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--start":
			if i+1 >= len(args) {
				return 0, 0, fmt.Errorf("--start requires a value")
			}
			i++
			start, err = strconv.Atoi(args[i])
			if err != nil {
				return 0, 0, fmt.Errorf("invalid --start value: %w", err)
			}
		case "--count":
			if i+1 >= len(args) {
				return 0, 0, fmt.Errorf("--count requires a value")
			}
			i++
			count, err = strconv.Atoi(args[i])
			if err != nil {
				return 0, 0, fmt.Errorf("invalid --count value: %w", err)
			}
		default:
			return 0, 0, fmt.Errorf("unknown flag: %s", args[i])
		}
	}
	if start < 1 || count < 1 {
		return 0, 0, fmt.Errorf("usage: analytics backfill --start <id> --count <n> (both must be positive)")
	}
	return start, count, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envIntOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		slog.Warn("invalid integer env var, using default", "key", key, "value", v, "default", fallback)
		return fallback
	}
	return n
}

func requireEnv(key string) (string, error) {
	v := os.Getenv(key)
	if v == "" {
		return "", fmt.Errorf("required env var not set: %s", key)
	}
	return v, nil
}
