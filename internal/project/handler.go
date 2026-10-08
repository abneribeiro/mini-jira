package project

import (
	"encoding/json"
	"fmt"
	"net/http"

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
	repo := NewRepository(db)
	service := NewService(repo)
	handler := NewHandler(service)

	r := chi.NewRouter()

	r.Post("/", handler.CreateProject)
	r.Get("/", handler.ListProjects)
	r.Get("/{id}", handler.GetProject)
	r.Put("/{id}", handler.UpdateProject)
	r.Delete("/{id}", handler.DeleteProject)

	return r
}

func (h *Handler) CreateProject(w http.ResponseWriter, r *http.Request) {
	var input CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request format"}`))
		return
	}
	defer r.Body.Close()

	createdProject, err := h.service.Create(r.Context(), input)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "%s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/projects/%s", createdProject.ID))
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(createdProject); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "failed to encode response"}`))
		return
	}
}

func (h *Handler) ListProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := h.service.List(r.Context())
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "%s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(projects); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "failed to encode response"}`))
		return
	}
}

func (h *Handler) GetProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid ID: must be a valid UUID"}`))
		return
	}

	project, err := h.service.GetByID(r.Context(), id)
	if err != nil {
		if err.Error() == fmt.Sprintf("repository.GetByID: project with id %s not found", id) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "project not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "%s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(project); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "failed to encode response"}`))
		return
	}
}

func (h *Handler) UpdateProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid ID: must be a valid UUID"}`))
		return
	}

	var input UpdateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid request format"}`))
		return
	}
	defer r.Body.Close()

	updatedProject, err := h.service.Update(r.Context(), id, input)
	if err != nil {
		if err.Error() == fmt.Sprintf("repository.Update: project with id %s not found", id) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "project not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error": "%s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(updatedProject); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error": "failed to encode response"}`))
		return
	}
}

func (h *Handler) DeleteProject(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error": "invalid ID: must be a valid UUID"}`))
		return
	}

	if err := h.service.Delete(r.Context(), id); err != nil {
		if err.Error() == fmt.Sprintf("repository.Delete: project with id %s not found", id) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"error": "project not found"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `{"error": "%s"}`, err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNoContent)
}
