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

	gh "github.com/openarbiter/arbiter/internal/github"
	"github.com/openarbiter/arbiter/internal/queue"
	"github.com/openarbiter/arbiter/internal/store"
)

func main() {
	logLevel := slog.LevelInfo
	if os.Getenv("ARBITER_LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx, cancel); err != nil && ctx.Err() == nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cancel context.CancelFunc) error {
	dbURL := requireEnv("ARBITER_DB_URL")
	redisURL := requireEnv("ARBITER_REDIS_URL")
	webhookSecret := requireEnv("ARBITER_WEBHOOK_SECRET")
	appIDStr := requireEnv("ARBITER_GITHUB_APP_ID")
	privateKeyPath := requireEnv("ARBITER_GITHUB_PRIVATE_KEY_PATH")

	appID, err := strconv.ParseInt(appIDStr, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid ARBITER_GITHUB_APP_ID: %w", err)
	}

	privateKeyPEM, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return fmt.Errorf("reading private key: %w", err)
	}

	listenAddr := os.Getenv("ARBITER_LISTEN_ADDR")
	if listenAddr == "" {
		listenAddr = ":8080"
	}

	// Initialize store
	slog.InfoContext(ctx, "connecting to database")
	pgStore, err := store.NewPgStore(ctx, dbURL)
	if err != nil {
		return fmt.Errorf("initializing store: %w", err)
	}
	defer pgStore.Close()

	// Initialize queue
	slog.InfoContext(ctx, "connecting to redis")
	q, err := queue.New(redisURL)
	if err != nil {
		return fmt.Errorf("initializing queue: %w", err)
	}
	defer q.Close()

	if err := q.Ping(ctx); err != nil {
		return fmt.Errorf("pinging redis: %w", err)
	}

	// Initialize GitHub client
	ghClient, err := gh.NewClient(appID, privateKeyPEM)
	if err != nil {
		return fmt.Errorf("initializing github client: %w", err)
	}

	// Set up routes
	mux := http.NewServeMux()
	mux.Handle("/webhook", gh.NewWebhookHandler(webhookSecret, q))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if err := pgStore.Ping(r.Context()); err != nil {
			http.Error(w, "database unhealthy", http.StatusServiceUnavailable)
			return
		}
		if err := q.Ping(r.Context()); err != nil {
			http.Error(w, "redis unhealthy", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, `{"status":"ok"}`)
	})

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start processor
	processor := gh.NewProcessor(pgStore, q, ghClient)
	go func() {
		if err := processor.Run(ctx); err != nil && ctx.Err() == nil {
			slog.Error("processor error", "error", err)
			cancel()
		}
	}()

	// Start HTTP server
	slog.InfoContext(ctx, "arbiter starting", "addr", listenAddr)
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
			cancel()
		}
	}()

	<-ctx.Done()
	slog.Info("arbiter shutting down")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	return server.Shutdown(shutdownCtx)
}

func requireEnv(key string) string {
	val := os.Getenv(key)
	if val == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return val
}
