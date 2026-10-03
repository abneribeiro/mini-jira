package server

import (
	"encoding/json"
	"net/http"

	"github.com/abneribeiro/internal/task"
	"github.com/abneribeiro/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
)

func New(db *pgxpool.Pool) http.Handler {
	r := chi.NewRouter()
	//Add middlewares
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)


	r.Get("/health", healthHandler)

	r.Mount("/tasks", task.Routes(db))
	r.Mount("/users", user.Routes(db))

	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Context-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}

