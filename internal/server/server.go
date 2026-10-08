package server

import (
	"encoding/json"
	"net/http"

	"github.com/abneribeiro/internal/auth"
	"github.com/abneribeiro/internal/jwt"
	// "github.com/abneribeiro/internal/metrics"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/abneribeiro/internal/project"
	"github.com/abneribeiro/internal/task"
	"github.com/abneribeiro/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

func New(db *pgxpool.Pool, jwtSecret string) http.Handler {
	r := chi.NewRouter()
	//Add middlewares
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(authmiddleware.MetricsMiddleware)

	r.Get("/health", healthHandler)
	r.Get("/metrics", promhttp.Handler().ServeHTTP)

	// Public routes
	r.Mount("/auth", auth.Routes(db, jwtSecret))
	r.Mount("/users", user.Routes(db))

	// Protected routes
	jwtManager := jwt.NewManager(jwtSecret)
	r.Group(func(r chi.Router) {
		r.Use(authmiddleware.AuthMiddleware(jwtManager))
		r.Mount("/tasks", task.Routes(db))
		r.Mount("/projects", project.Routes(db))
	})

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

