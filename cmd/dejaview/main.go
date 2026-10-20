package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/drywaters/dejaview/internal/assets"
	"github.com/drywaters/dejaview/internal/config"
	"github.com/drywaters/dejaview/internal/metrics"
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
	tmdbClient := tmdb.NewClient(cfg.TMDBReadAccessToken)
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

	// Metrics are recorded even when the listener is off, which keeps the
	// router identical either way.
	appMetrics := metrics.New()
	if err := appMetrics.Register(metrics.NewPgxPoolCollector(pool)); err != nil {
		return fmt.Errorf("failed to register pool metrics: %w", err)
	}

	// Create server
	srv := server.New(cfg, pool, movieRepo, entryRepo, personRepo, ratingRepo, statsRepo, tmdbClient, appMetrics)

	// Start HTTP server
	httpServer := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      srv.Router(),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Internal metrics listener, separate from the public router.
	var metricsServer *http.Server
	if cfg.MetricsAddr != "" {
		metricsServer = &http.Server{
			Addr:              cfg.MetricsAddr,
			Handler:           appMetrics.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
			WriteTimeout:      15 * time.Second,
			IdleTimeout:       60 * time.Second,
		}
	} else {
		slog.Info("metrics listener disabled")
	}

	// Graceful shutdown
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return serve(sigCtx, httpServer, metricsServer)
}

// serve runs httpServer, and metricsServer when it is not nil, until ctx is
// done, then shuts both down gracefully. It returns promptly with an error if
// httpServer fails to start or stops unexpectedly (e.g. the port is already
// in use). Metrics server failures are logged, never fatal.
func serve(ctx context.Context, httpServer, metricsServer *http.Server) error {
	// Bind the public port first so the optional metrics listener can never
	// take it, e.g. when PORT=9464 and METRICS_ADDR is left at its default.
	ln, err := net.Listen("tcp", httpServer.Addr)
	if err != nil {
		return fmt.Errorf("server error: %w", err)
	}

	if metricsServer != nil {
		startMetricsServer(metricsServer)
		defer func() {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := metricsServer.Shutdown(shutdownCtx); err != nil {
				slog.Warn("metrics server shutdown error", "error", err)
			}
		}()
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("server listening", "addr", httpServer.Addr)
		serverErr <- httpServer.Serve(ln)
	}()

	select {
	case err := <-serverErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("server error: %w", err)
	case <-ctx.Done():
	}
	slog.Info("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("server shutdown error: %w", err)
	}

	slog.Info("server stopped")
	return nil
}

// startMetricsServer binds the metrics listener and serves it in the
// background. A bind failure is logged and the app runs without metrics.
func startMetricsServer(metricsServer *http.Server) {
	ln, err := net.Listen("tcp", metricsServer.Addr)
	if err != nil {
		slog.Error("metrics listener failed; continuing without metrics", "addr", metricsServer.Addr, "error", err)
		return
	}
	slog.Info("metrics listening", "addr", ln.Addr().String())
	go func() {
		if err := metricsServer.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("metrics server error", "error", err)
		}
	}()
}
