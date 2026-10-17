package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/abneribeiro/internal/auth"
	"github.com/abneribeiro/internal/httpx"
	"github.com/abneribeiro/internal/jwt"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/abneribeiro/internal/project"
	"github.com/abneribeiro/internal/task"
	"github.com/abneribeiro/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// healthPingTimeout bounds the readiness probe so a hung database cannot hold
// the health endpoint open.
const healthPingTimeout = 2 * time.Second

// Deps are what the routers need from main.
type Deps struct {
	DB        *pgxpool.Pool
	JWTSecret string
	Limiters  auth.Limiters

	// TrustProxyHeaders mounts chi's RealIP. Leave it off unless a proxy that
	// rewrites X-Forwarded-For sits in front: the auth rate limiters key on
	// the client address, and a spoofable address is no limit at all.
	TrustProxyHeaders bool
}

// New builds the public API. /metrics is deliberately NOT mounted here; it
// lives on the separate, unpublished listener returned by NewMetrics.
func New(deps Deps) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	if deps.TrustProxyHeaders {
		r.Use(middleware.RealIP)
	}
	r.Use(authmiddleware.RequestLogger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))
	r.Use(authmiddleware.MetricsMiddleware)

	r.NotFound(notFoundHandler)
	r.MethodNotAllowed(methodNotAllowedHandler)

	r.Get("/health", healthHandler(deps.DB))
	r.Get("/livez", livezHandler)

	// One manager for the whole app: two instances would drift the moment
	// issuer, TTL or key rotation becomes configurable.
	jwtManager := jwt.NewManager(deps.JWTSecret)

	// Public routes
	r.Mount("/auth", auth.Routes(deps.DB, jwtManager, deps.Limiters))

	// Mixed: POST /users is public registration, the rest are protected
	// inside user.Routes.
	// The refresh repository is handed to the user package as its
	// SessionRevoker: a password change ends every session. Wiring it here
	// keeps user from importing auth, which already imports user.
	r.Mount("/users", user.Routes(deps.DB, jwtManager, auth.NewRefreshRepository(deps.DB)))

	// Protected routes
	r.Group(func(r chi.Router) {
		r.Use(authmiddleware.AuthMiddleware(jwtManager))
		r.Mount("/tasks", task.Routes(deps.DB))
		r.Mount("/projects", project.Routes(deps.DB))
	})

	return r
}

// NewMetrics builds the private operator surface. It is served on its own
// listener that the Compose file does not publish to the host, so the
// exposition (route inventory, traffic volumes, Go runtime internals) is
// reachable from Prometheus on the internal network and from nowhere else.
func NewMetrics(db *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.Recoverer)

	r.NotFound(notFoundHandler)
	r.MethodNotAllowed(methodNotAllowedHandler)

	r.Get("/metrics", promhttp.Handler().ServeHTTP)
	r.Get("/health", healthHandler(db))
	r.Get("/livez", livezHandler)

	return r
}

// healthHandler is a readiness probe: it reports unhealthy when the database
// is unreachable, instead of claiming "ok" while every request 500s.
func healthHandler(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
		defer cancel()

		if err := db.Ping(ctx); err != nil {
			slog.ErrorContext(r.Context(), "health check: database unreachable", "error", err)
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status":   "degraded",
				"database": "unreachable",
			})
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]string{
			"status":   "ok",
			"database": "ok",
		})
	}
}

// livezHandler is a liveness probe: it answers as long as the process can
// serve, regardless of dependencies.
func livezHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// notFoundHandler and methodNotAllowedHandler replace chi's text/plain
// defaults, so every error the API emits uses the same JSON envelope.
// Mounted sub-routers inherit them.
func notFoundHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, http.StatusNotFound, httpx.CodeNotFound, "route not found")
}

func methodNotAllowedHandler(w http.ResponseWriter, _ *http.Request) {
	httpx.WriteError(w, http.StatusMethodNotAllowed, httpx.CodeMethodNotAllowed, "method not allowed")
}
