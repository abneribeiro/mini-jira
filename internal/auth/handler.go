package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/abneribeiro/internal/httpx"
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

func Routes(db *pgxpool.Pool, jwtManager *jwt.Manager) chi.Router {
	userRepo := user.NewRepository(db)
	service := NewService(userRepo, jwtManager)
	handler := NewHandler(service)

	r := chi.NewRouter()

	r.Post("/login", handler.Login)

	return r
}

func (h *Handler) Login(w http.ResponseWriter, r *http.Request) {
	var input LoginInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	response, err := h.service.Login(r.Context(), input)
	if err != nil {
		// Both credential failures share one message so the response cannot be
		// used to tell a missing account from a wrong password.
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrUserNotFound) {
			httpx.WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}

		slog.ErrorContext(r.Context(), "login failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}
