package auth

import (
	"encoding/json"
	"net/http"

	"github.com/abneribeiro/internal/jwt"
	"github.com/abneribeiro/internal/user"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func Routes(db *pgxpool.Pool, jwtSecret string) chi.Router {
	userRepo := user.NewRepository(db)
	jwtManager := jwt.NewManager(jwtSecret)
	service := NewService(userRepo, jwtManager)
	handler := NewHandler(service)

	r := chi.NewRouter()

	r.Post("/login", handler.Login)

	return r
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request format"}`))
		return
	}
	defer r.Body.Close()

	response, err := h.service.Login(r.Context(), input)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		if err == ErrInvalidCredentials || err == ErrUserNotFound {
			w.WriteHeader(http.StatusUnauthorized)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
		w.Write([]byte(`{"error": "invalid credentials"}`))
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "failed to encode response"}`))
		return
	}
}
