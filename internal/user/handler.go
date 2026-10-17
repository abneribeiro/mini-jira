package user

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/httpx"
	"github.com/abneribeiro/internal/jwt"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func Routes(db *pgxpool.Pool, jwtManager *jwt.Manager, sessions SessionRevoker) chi.Router {
	return routes(NewHandler(NewService(NewRepository(db), sessions)), jwtManager)
}

// routes is split out so tests can mount the same routing and middleware
// against a stub service instead of a live pool.
func routes(handler *Handler, jwtManager *jwt.Manager) chi.Router {
	r := chi.NewRouter()

	r.Post("/", handler.CreateUser)
	r.Group(func(r chi.Router) {
		r.Use(authmiddleware.AuthMiddleware(jwtManager))
		r.Get("/", handler.ListUsers)
		r.Get("/{id}", handler.GetUser)
		r.Put("/{id}", handler.UpdateUser)
		r.Delete("/{id}", handler.DeleteUser)

		// Promotion is admin-only and lives on its own route, so a role can
		// never ride along in an ordinary profile update.
		r.With(authmiddleware.RequireAdmin).Patch("/{id}/role", handler.UpdateUserRole)
	})

	return r
}

// respondError is the single place that turns a domain error into a status.
// Anything unrecognised is a server fault: it is logged in full and the client
// gets a generic message.
func respondError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUserNotFound):
		httpx.WriteError(w, http.StatusNotFound, "user_not_found", "user not found")
	case errors.Is(err, ErrEmailAlreadyExists):
		httpx.WriteError(w, http.StatusConflict, "email_already_exists", "email already registered")
	case errors.Is(err, ErrNoFieldsToUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "no_fields_to_update", "no fields to update")
	case errors.Is(err, ErrInvalidUser), errors.Is(err, ErrInvalidRole):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidation, err.Error())
	default:
		slog.ErrorContext(r.Context(), "unhandled user error", "error", err, "path", r.URL.Path)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
	}
}

// requireSelfOrAdmin enforces that a member may only mutate their own
// account; an admin may act on any.
func requireSelfOrAdmin(w http.ResponseWriter, r *http.Request, target uuid.UUID) bool {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return false
	}

	if !scope.CanAccess(target) {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "you can only modify your own account")
		return false
	}

	return true
}

// parseListFilter reads pagination from the query string.
func parseListFilter(r *http.Request) (ListFilter, error) {
	limit, offset, err := httpx.ParsePagination(r, DefaultListLimit)
	if err != nil {
		return ListFilter{}, fmt.Errorf("%w: %w", ErrInvalidUser, err)
	}

	return ListFilter{Limit: limit, Offset: offset}, nil
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	createdUser, err := h.service.Create(r.Context(), input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/users/%s", createdUser.ID))
	httpx.WriteJSON(w, http.StatusCreated, createdUser)
}

func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	if !requireSelfOrAdmin(w, r, id) {
		return
	}

	var input UpdateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	updatedUser, err := h.service.Update(r.Context(), id, input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updatedUser)
}

func (h *Handler) ListUsers(w http.ResponseWriter, r *http.Request) {
	filter, err := parseListFilter(r)
	if err != nil {
		respondError(w, r, err)
		return
	}

	users, err := h.service.List(r.Context(), filter)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, users)
}

func (h *Handler) GetUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	user, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, user)
}

func (h *Handler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	if !requireSelfOrAdmin(w, r, id) {
		return
	}

	if err := h.service.Delete(r.Context(), id); err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteNoContent(w)
}

// UpdateUserRole is mounted behind RequireAdmin.
func (h *Handler) UpdateUserRole(w http.ResponseWriter, r *http.Request) {
	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	var input struct {
		Role string `json:"role"`
	}
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	caller, ok := authmiddleware.GetUserIDFromContext(r.Context())
	if !ok {
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "authentication required")
		return
	}

	// An admin demoting themselves could leave the system with no admin and
	// no way back in through the API.
	if caller == id && input.Role != authz.RoleAdmin {
		httpx.WriteError(w, http.StatusForbidden, httpx.CodeForbidden, "you cannot remove your own admin role")
		return
	}

	updated, err := h.service.UpdateRole(r.Context(), id, input.Role)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updated)
}
