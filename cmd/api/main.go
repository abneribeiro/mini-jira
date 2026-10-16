package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/abneribeiro/internal/auth"
	"github.com/abneribeiro/internal/config"
	"github.com/abneribeiro/internal/database"
	"github.com/abneribeiro/internal/metrics"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/abneribeiro/internal/server"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
	metricsInterval   = 30 * time.Second
	tokenPruneEvery   = 1 * time.Hour
)

func main() {
	// run() owns every defer. main() only reports, so nothing is skipped by an
	// os.Exit in the middle of the shutdown path.
	if err := run(); err != nil {
		slog.Error("fatal", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	slog.SetDefault(newLogger(cfg.AppEnv))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := database.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer db.Close()

	go metrics.NewCollector(db, metricsInterval).Run(ctx)

	loginLimiter := authmiddleware.NewRateLimiter(authmiddleware.LoginRateLimit())
	refreshLimiter := authmiddleware.NewRateLimiter(authmiddleware.RefreshRateLimit())
	go loginLimiter.Run(ctx.Done())
	go refreshLimiter.Run(ctx.Done())

	go pruneRefreshTokens(ctx, auth.NewRefreshRepository(db))

	apiSrv := newServer(":"+cfg.AppPort, server.New(server.Deps{
		DB:        db,
		JWTSecret: cfg.JWTSecret,
		Limiters:  auth.Limiters{Login: loginLimiter, Refresh: refreshLimiter},
	}))

	// Separate listener: the Compose file does not publish this port, so the
	// metrics exposition stays on the internal network.
	metricsSrv := newServer(":"+cfg.MetricsPort, server.NewMetrics(db))

	serveErr := make(chan error, 2)

	go func() {
		slog.Info("api listening", "port", cfg.AppPort, "env", cfg.AppEnv)
		if err := apiSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("api server: %w", err)
		}
	}()

	go func() {
		slog.Info("metrics listening (private)", "port", cfg.MetricsPort)
		if err := metricsSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- fmt.Errorf("metrics server: %w", err)
		}
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining in-flight requests")
	}

	// A fresh context: the signal already cancelled ctx, so reusing it would
	// abort the drain immediately.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return shutdownAll(shutdownCtx, apiSrv, metricsSrv)
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:    addr,
		Handler: handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
}

func shutdownAll(ctx context.Context, servers ...*http.Server) error {
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)

	for _, srv := range servers {
		wg.Add(1)
		go func(srv *http.Server) {
			defer wg.Done()
			if err := srv.Shutdown(ctx); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(srv)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("graceful shutdown: %w", errors.Join(errs...))
	}

	slog.Info("servers stopped cleanly")

	return nil
}

// pruneRefreshTokens keeps the refresh_tokens table from growing without
// bound as sessions expire.
func pruneRefreshTokens(ctx context.Context, repo auth.RefreshRepository) {
	ticker := time.NewTicker(tokenPruneEvery)
	defer ticker.Stop()

	prune := func() {
		pruneCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()

		deleted, err := repo.DeleteExpired(pruneCtx, time.Now())
		if err != nil {
			slog.WarnContext(ctx, "prune refresh tokens", "error", err)
			return
		}
		if deleted > 0 {
			slog.InfoContext(ctx, "pruned expired refresh tokens", "count", deleted)
		}
	}

	prune()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		}
	}
}

func newLogger(appEnv string) *slog.Logger {
	if appEnv == "local" {
		return slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}
