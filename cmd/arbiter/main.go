package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	gh "github.com/openarbiter/arbiter/internal/github"
	"github.com/openarbiter/arbiter/internal/queue"
	"github.com/openarbiter/arbiter/internal/store"
)

// version is set at build time via -ldflags.
var version = "dev"

func main() {
	logLevel := slog.LevelInfo
	if os.Getenv("ARBITER_LOG_LEVEL") == "debug" {
		logLevel = slog.LevelDebug
	}
	jsonHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(gh.NewCorrelationHandler(jsonHandler))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// Check for subcommands
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		if err := runMigrate(ctx); err != nil {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
		return
	}

	if err := run(ctx, cancel); err != nil && ctx.Err() == nil {
		slog.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func runMigrate(ctx context.Context) error {
	slog.Info("migrate: starting (only ARBITER_DB_URL required)")

	dbURL := os.Getenv("ARBITER_DB_URL")
	if dbURL == "" {
		return fmt.Errorf("ARBITER_DB_URL is required for migrate")
	}
	migrationsDir := os.Getenv("ARBITER_MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "/migrations"
	}

	slog.Info("running migrations", "dir", migrationsDir)

	// Retry connecting to Postgres — it may still be starting up
	var pool *pgxpool.Pool
	for attempt := 1; attempt <= 30; attempt++ {
		var err error
		pool, err = pgxpool.New(ctx, dbURL)
		if err == nil {
			if pingErr := pool.Ping(ctx); pingErr == nil {
				break
			}
			pool.Close()
		}
		if attempt == 30 {
			return fmt.Errorf("could not connect to database after 30 attempts: %w", err)
		}
		slog.Info("waiting for database...", "attempt", attempt)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Second):
		}
	}
	defer pool.Close()
	slog.Info("database connected")

	entries, err := os.ReadDir(migrationsDir)
	if err != nil {
		return fmt.Errorf("reading migrations dir %s: %w", migrationsDir, err)
	}

	if len(entries) == 0 {
		slog.Warn("no migration files found", "dir", migrationsDir)
		return nil
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		path := migrationsDir + "/" + entry.Name()
		slog.Info("applying migration", "file", entry.Name())

		sql, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", entry.Name(), err)
		}

		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			return fmt.Errorf("executing %s: %w", entry.Name(), err)
		}

		slog.Info("migration applied", "file", entry.Name())
	}

	slog.Info("all migrations complete")
	return nil
}

func run(ctx context.Context, cancel context.CancelFunc) error {
	dbURL := requireEnv("ARBITER_DB_URL")
	redisURL := requireEnv("ARBITER_REDIS_URL")
	webhookSecret := requireEnv("ARBITER_WEBHOOK_SECRET")

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

	// Initialize GitHub client (optional — needed for webhook processing)
	var ghClient *gh.Client
	appIDStr := os.Getenv("ARBITER_GITHUB_APP_ID")
	privateKeyPath := os.Getenv("ARBITER_GITHUB_PRIVATE_KEY_PATH")
	if appIDStr != "" && privateKeyPath != "" {
		appID, err := strconv.ParseInt(appIDStr, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid ARBITER_GITHUB_APP_ID: %w", err)
		}
		privateKeyPEM, err := os.ReadFile(privateKeyPath)
		if err != nil {
			return fmt.Errorf("reading private key: %w", err)
		}
		ghClient, err = gh.NewClient(appID, privateKeyPEM)
		if err != nil {
			return fmt.Errorf("initializing github client: %w", err)
		}
		slog.InfoContext(ctx, "github client initialized", "app_id", appID)
	} else {
		slog.WarnContext(ctx, "github client not configured — webhook processing disabled")
	}

	// Initialize stats
	stats := gh.NewStats()

	// Set up routes
	mux := http.NewServeMux()
	rateLimiter := gh.NewRateLimiter(100, 200) // 100 req/s, burst 200
	mux.Handle("/webhook", rateLimiter.Middleware(gh.NewWebhookHandler(webhookSecret, q, stats)))
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		status := struct {
			Status  string `json:"status"`
			Version string `json:"version"`
			Uptime  string `json:"uptime"`
			DB      string `json:"db"`
			Redis   string `json:"redis"`
		}{
			Status:  "ok",
			Version: version,
			Uptime:  stats.Snapshot().Uptime,
			DB:      "ok",
			Redis:   "ok",
		}
		if err := pgStore.Ping(r.Context()); err != nil {
			status.Status = "degraded"
			status.DB = "unhealthy"
		}
		if err := q.Ping(r.Context()); err != nil {
			status.Status = "degraded"
			status.Redis = "unhealthy"
		}
		w.Header().Set("Content-Type", "application/json")
		if status.Status != "ok" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(status)
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		snapshot := stats.Snapshot()
		queueLen, _ := q.Len(r.Context())
		deadLen, _ := q.DeadLetterLen(r.Context())

		resp := struct {
			gh.StatsSnapshot
			QueueDepth    int64 `json:"queue_depth"`
			DeadLetterLen int64 `json:"dead_letter_len"`
		}{
			StatsSnapshot: snapshot,
			QueueDepth:    queueLen,
			DeadLetterLen: deadLen,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	// API endpoints — read endpoints are open, write endpoints require GitHub token
	api := gh.NewAPI(pgStore)
	mux.HandleFunc("/api/proposals", api.HandleListProposals)
	mux.Handle("/api/challenge", gh.NewAPIAuth(http.HandlerFunc(api.HandleCreateChallenge)))
	mux.Handle("/api/challenge/resolve", gh.NewAPIAuth(http.HandlerFunc(api.HandleResolveChallenge)))

	server := &http.Server{
		Addr:         listenAddr,
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Start processor
	processor := gh.NewProcessor(pgStore, q, ghClient, stats)
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
