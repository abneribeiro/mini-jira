package task

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/abneribeiro/internal/authz"
	"github.com/abneribeiro/internal/httpx"
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

func Routes(db *pgxpool.Pool) chi.Router {
	return routes(NewHandler(NewService(NewRepository(db))))
}

// routes is split out so tests can mount the same routing against a stub
// service instead of a live pool.
func routes(handler *Handler) chi.Router {
	r := chi.NewRouter()

	r.Post("/", handler.CreateTask)
	r.Get("/", handler.ListTasks)
	r.Get("/{id}", handler.GetTask)
	r.Put("/{id}", handler.UpdateTask)
	r.Delete("/{id}", handler.DeleteTask)

	return r
}

func respondError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTaskNotFound):
		httpx.WriteError(w, http.StatusNotFound, "task_not_found", "task not found")
	// The task itself is addressable; a bad project_id or assignee_id is an
	// unprocessable reference in the body, not a missing resource.
	case errors.Is(err, ErrProjectNotFound):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "project_not_found", "project not found")
	case errors.Is(err, ErrAssigneeNotFound):
		httpx.WriteError(w, http.StatusUnprocessableEntity, "assignee_not_found", "assignee not found")
	case errors.Is(err, ErrNoFieldsToUpdate):
		httpx.WriteError(w, http.StatusBadRequest, "no_fields_to_update", "no fields to update")
	case errors.Is(err, ErrInvalidTask):
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidation, err.Error())
	default:
		slog.ErrorContext(r.Context(), "unhandled task error", "error", err, "path", r.URL.Path)
		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternal, "internal server error")
	}
}

// callerID is the authenticated user. AuthMiddleware guarantees it is
// present on every route in this package; a miss means the route was mounted
// without auth, which is a wiring bug, not a client error.
func callerScope(w http.ResponseWriter, r *http.Request) (authz.Scope, bool) {
	scope, ok := authmiddleware.ScopeFromContext(r.Context())
	if !ok {
		slog.ErrorContext(r.Context(), "task route reached without an authenticated user", "path", r.URL.Path)
		httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "authentication required")
		return authz.Scope{}, false
	}

	return scope, true
}

// parseListFilter reads the query string. The service validates the values;
// this only rejects inputs that cannot be parsed at all.
func parseListFilter(r *http.Request) (ListFilter, error) {
	q := r.URL.Query()
	filter := ListFilter{Limit: DefaultListLimit}

	if raw := q.Get("project_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("%w: project_id must be a valid UUID", ErrInvalidTask)
		}
		filter.ProjectID = &id
	}

	if raw := q.Get("assignee_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("%w: assignee_id must be a valid UUID", ErrInvalidTask)
		}
		filter.AssigneeID = &id
	}

	if raw := q.Get("status"); raw != "" {
		filter.Status = &raw
	}

	if raw := q.Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("%w: limit must be an integer", ErrInvalidTask)
		}
		filter.Limit = limit
	}

	if raw := q.Get("offset"); raw != "" {
		offset, err := strconv.Atoi(raw)
		if err != nil {
			return ListFilter{}, fmt.Errorf("%w: offset must be an integer", ErrInvalidTask)
		}
		filter.Offset = offset
	}

	return filter, nil
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidID, "invalid ID: must be a valid UUID")
		return uuid.Nil, false
	}

	return id, true
}

func (h *Handler) CreateTask(w http.ResponseWriter, r *http.Request) {
	scope, ok := callerScope(w, r)
	if !ok {
		return
	}

	var input CreateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	createdTask, err := h.service.Create(r.Context(), scope, input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	w.Header().Set("Location", fmt.Sprintf("/tasks/%s", createdTask.ID))
	httpx.WriteJSON(w, http.StatusCreated, createdTask)
}

func (h *Handler) ListTasks(w http.ResponseWriter, r *http.Request) {
	scope, ok := callerScope(w, r)
	if !ok {
		return
	}

	filter, err := parseListFilter(r)
	if err != nil {
		respondError(w, r, err)
		return
	}

	tasks, err := h.service.List(r.Context(), scope, filter)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, tasks)
}

func (h *Handler) GetTask(w http.ResponseWriter, r *http.Request) {
	scope, ok := callerScope(w, r)
	if !ok {
		return
	}

	id, ok := parseID(w, r)
	if !ok {
		return
	}

	task, err := h.service.GetByID(r.Context(), id, scope)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, task)
}

func (h *Handler) UpdateTask(w http.ResponseWriter, r *http.Request) {
	scope, ok := callerScope(w, r)
	if !ok {
		return
	}

	id, ok := parseID(w, r)
	if !ok {
		return
	}

	var input UpdateInput
	if err := httpx.DecodeJSON(w, r, &input); err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid request format")
		return
	}

	updatedTask, err := h.service.Update(r.Context(), id, scope, input)
	if err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, updatedTask)
}

func (h *Handler) DeleteTask(w http.ResponseWriter, r *http.Request) {
	scope, ok := callerScope(w, r)
	if !ok {
		return
	}

	id, ok := parseID(w, r)
	if !ok {
		return
	}

	if err := h.service.Delete(r.Context(), id, scope); err != nil {
		respondError(w, r, err)
		return
	}

	httpx.WriteNoContent(w)
}
