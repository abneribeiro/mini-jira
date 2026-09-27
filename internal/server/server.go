package server

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func New() http.Handler {
	r := chi.NewRouter()
	//Add middlewares
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)


	r.Get("/health", healthHandler)
	// r.Mount("/users", users.Routes())
	
	return r
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Context-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
