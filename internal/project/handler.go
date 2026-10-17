package project

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/abneribeiro/internal/httpx"
	authmiddleware "github.com/abneribeiro/internal/middleware"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func Routes(db *pgxpool.Pool) chi.Router {
	return routes(NewHandler(NewService(NewRepository(db))))
}

// routes is split out so tests can mount the same routing against a stub
// service instead of a live pool.
func routes(handler *Handler) chi.Router {
	r := chi.NewRouter()

	r.Post("/", handler.CreateProject)
	r.Get("/", handler.ListProjects)
	r.Get("/{id}", handler.GetProject)
	r.Put("/{id}", handler.UpdateProject)
	r.Delete("/{id}", handler.DeleteProject)

	return r
}

func respondError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrProjectNotFound):
		httpx.WriteError(w, http.StatusNotFound, "project_not_found", "project not found")
	case errors.Is(err, ErrNoFieldsToUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "no_fields_to_update", "no fields to update")
	case errors.Is(err, ErrInvalidProject):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidation, err.Error())
	default:
		slog.ErrorContext(r.Context(), "unhandled project error", "error", err, "path", r.URL.Path)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
	}
}

// parseListFilter reads pagination from the query string.
func parseListFilter(r *http.Request) (ListFilter, error) {
	limit, offset, err := httpx.ParsePagination(r, DefaultListLimit)
	if err != nil {
		return ListFilter{}, fmt.Errorf("%w: %w", ErrInvalidProject, err)
	}

	return ListFilter{Limit: limit, Offset: offset}, nil
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return
	}

	var input CreateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	createdProject, err := h.service.Create(r.Context(), scope, input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/projects/%s", createdProject.ID))
	httpx.WriteJSON(w, http.StatusCreated, createdProject)
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return
	}

	filter, err := parseListFilter(r)
	if err != nil {
		respondError(w, r, err)
		return
	}

	projects, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, projects)
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return
	}

	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	project, err := h.service.GetByID(r.Context(), id, scope)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, project)
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return
	}

	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	var input UpdateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	updatedProject, err := h.service.Update(r.Context(), id, scope, input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updatedProject)
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	scope, ok := authmiddleware.RequireScope(w, r)
	if !ok {
		return
	}

	id, ok := httpx.URLParamUUID(w, r, "id")
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), id, scope); err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteNoContent(w)
}
