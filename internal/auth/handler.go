package auth

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/abneribeiro/internal/httpx"
	"github.com/abneribeiro/internal/jwt"
	authmiddleware "github.com/abneribeiro/internal/middleware"
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

// Limiters carries the two rate limits the auth routes apply. They are
// separate because the credentials they protect have very different
// guessability.
type Limiters struct {
	Login   *authmiddleware.RateLimiter
	Refresh *authmiddleware.RateLimiter
}

func Routes(db *pgxpool.Pool, jwtManager *jwt.Manager, limiters Limiters) chi.Router {
	service := NewService(user.NewRepository(db), NewRefreshRepository(db), jwtManager)

	return routes(NewHandler(service), limiters)
}

// routes is split out so tests can mount the same routing against a stub.
func routes(handler *Handler, limiters Limiters) chi.Router {
	r := chi.NewRouter()

	// Password submission: strict, because passwords are guessable and each
	// attempt costs us a bcrypt hash.
	r.Group(func(r chi.Router) {
		if limiters.Login != nil {
			r.Use(limiters.Login.Middleware)
		}
		r.Post("/login", handler.Login)
	})

	// Token rotation: looser. Legitimate clients refresh on every access
	// token expiry, and the token itself is high-entropy.
	r.Group(func(r chi.Router) {
		if limiters.Refresh != nil {
			r.Use(limiters.Refresh.Middleware)
		}
		r.Post("/refresh", handler.Refresh)
	})

	// Logout needs a token to do anything and is idempotent.
	r.Post("/logout", handler.Logout)

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
		// Both credential failures share one message so the response cannot
		// be used to tell a missing account from a wrong password.
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

func (h *Handler) Refresh(w http.ResponseWriter, r *http.Request) {
	var input RefreshInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	response, err := h.service.Refresh(r.Context(), input)
	if err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			httpx.WriteError(w, http.StatusUnauthorized, "invalid_refresh_token", "invalid or expired refresh token")
			return
		}

		slog.ErrorContext(r.Context(), "refresh failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
		return
	}

	httpx.WriteJSON(w, http.StatusOK, response)
}

func (h *Handler) Logout(w http.ResponseWriter, r *http.Request) {
	var input RefreshInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	if err := h.service.Logout(r.Context(), input); err != nil {
		if errors.Is(err, ErrInvalidRefresh) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "refresh_token is required")
			return
		}

		slog.ErrorContext(r.Context(), "logout failed", "error", err)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
		return
	}

	httpx.WriteNoContent(w)
}
