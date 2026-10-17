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
	healthcheckWait   = 3 * time.Second
)

// version is stamped at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	// The runtime image is distroless and has no shell or curl, so the
	// container healthcheck runs the binary itself: `api healthcheck`.
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		if err := healthcheck(); err != nil {
			fmt.Fprintln(os.Stderr, "healthcheck failed:", err)
			os.Exit(1)
		}
		return
	}

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

	// Background workers stop on ctx and are joined before the deferred
	// db.Close, so none of them is mid-query when the pool goes away.
	var workers sync.WaitGroup
	defer workers.Wait()

	workers.Go(func() { metrics.NewCollector(db, metricsInterval).Run(ctx) })

	loginLimiter := authmiddleware.NewRateLimiter(authmiddleware.LoginRateLimit())
	refreshLimiter := authmiddleware.NewRateLimiter(authmiddleware.RefreshRateLimit())
	workers.Go(func() { loginLimiter.Run(ctx.Done()) })
	workers.Go(func() { refreshLimiter.Run(ctx.Done()) })

	workers.Go(func() { pruneRefreshTokens(ctx, auth.NewRefreshRepository(db)) })

	apiSrv := newServer(":"+cfg.AppPort, server.New(server.Deps{
		DB:                db,
		JWTSecret:         cfg.JWTSecret,
		Limiters:          auth.Limiters{Login: loginLimiter, Refresh: refreshLimiter},
		TrustProxyHeaders: cfg.TrustProxyHeaders,
	}))

	// Separate listener: the Compose file does not publish this port, so the
	// metrics exposition stays on the internal network.
	metricsSrv := newServer(":"+cfg.MetricsPort, server.NewMetrics(db))

	serveErr := make(chan error, 2)

	go func() {
		slog.Info("api listening", "port", cfg.AppPort, "env", cfg.AppEnv, "version", version)
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

	var listenErr error
	select {
	case listenErr = <-serveErr:
		// Release the workers too, or the deferred Wait would block forever.
		// The other listener may still be serving, so it is drained below
		// before the pool closes.
		stop()
		slog.Error("listener failed, shutting down", "error", listenErr)
	case <-ctx.Done():
		slog.Info("shutdown signal received, draining in-flight requests")
	}

	// A fresh context: ctx is already cancelled, so reusing it would abort
	// the drain immediately.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return errors.Join(listenErr, shutdownAll(shutdownCtx, apiSrv, metricsSrv))
}

func newServer(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
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
		wg.Go(func() {
			if err := srv.Shutdown(ctx); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		})
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

// healthcheck probes the liveness endpoint of a server running in the same
// container. It reads PORT directly instead of config.Load, which would
// demand DATABASE_URL and JWT_SECRET for a loopback GET.
func healthcheck() error {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	client := &http.Client{Timeout: healthcheckWait}
	resp, err := client.Get("http://127.0.0.1:" + port + "/livez")
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/livez returned %d", resp.StatusCode)
	}

	return nil
}

func newLogger(appEnv string) *slog.Logger {
	if appEnv == "local" {
		return slog.New(slog.NewTextHandler(os.Stdout, nil))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}
