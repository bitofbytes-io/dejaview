package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/drywaters/dejaview/internal/assets"
	"github.com/drywaters/dejaview/internal/config"
	"github.com/drywaters/dejaview/internal/repository"
	"github.com/drywaters/dejaview/internal/server"
	"github.com/drywaters/dejaview/internal/tmdb"
	"github.com/drywaters/dejaview/internal/ui/layout"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	version  = "dev"
	revision = "unknown"
)

func main() {
	if err := run(); err != nil {
		slog.Error("application error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Set up logging
	logLevel := slog.LevelInfo
	logLevels := map[string]slog.Level{
		"debug": slog.LevelDebug,
		"info":  slog.LevelInfo,
		"warn":  slog.LevelWarn,
		"error": slog.LevelError,
	}
	if level, ok := logLevels[cfg.LogLevel]; ok {
		logLevel = level
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
	slog.SetDefault(logger)

	slog.Info("starting dejaview", "port", cfg.Port, "version", version, "revision", revision)

	// Connect to database
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to database: %w", err)
	}
	defer pool.Close()

	// Verify database connection
	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}
	slog.Info("connected to database")

	// Initialize repositories
	movieRepo := repository.NewMovieRepository(pool)
	entryRepo := repository.NewEntryRepository(pool)
	personRepo := repository.NewPersonRepository(pool)
	ratingRepo := repository.NewRatingRepository(pool)
	statsRepo := repository.NewStatsRepository(pool)

	// Initialize TMDB client
	tmdbClient := tmdb.NewClient(cfg.TMDBAPIKey)
	slog.Info("TMDB client initialized")

	assetsVersion, err := assets.Version(
		filepath.Join("static", "styles.css"),
		filepath.Join("static", "dragdrop.js"),
		filepath.Join("static", "htmx.min.js"),
	)
	if err != nil {
		slog.Warn("asset version unavailable", "error", err)
	} else {
		layout.SetAssetsVersion(assetsVersion)
	}

	// Create server
	srv := server.New(cfg, movieRepo, entryRepo, personRepo, ratingRepo, statsRepo, tmdbClient)

	// Start HTTP server
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown
	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "addr", httpServer.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
		}
	}()

	<-shutdownChan
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}
