package user

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service Service
}

func NewHandler(service Service) *Handler {
	return &Handler{service: service}
}

func Routes( db *pgxpool.Pool) chi.Router {
	repo := NewRepository(db)
	service := NewService(repo)
	handler := NewHandler(service)

	r := chi.NewRouter()

	r.Post("/", handler.CreateUser)
	r.Put("/{id}", handler.UpdateUser)
	// r.Get("/", handler.ListTasks)

	return r
}

func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {

	var input CreateInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error": "invalid request format"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	createdUser, err := h.service.Create(r.Context(), input)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/users/%d", createdUser.ID))
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(createdUser); err != nil {
		http.Error(w, `{"error": "failed to encode response"}`, http.StatusInternalServerError)
		return
	}
}


func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var input UpdateInput

	idStr := chi.URLParam(r, "id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, `{"error": "invalid ID: must be a number"}`, http.StatusBadRequest)
		return
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, `{"error": "invalid request format"}`, http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	updateUser, err := h.service.Update(r.Context(), id, input)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error": "%s"}`, err.Error()), http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/users/%d", updateUser.ID))
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(updateUser); err != nil {
		http.Error(w, `{"error": "failed to encode response"}`, http.StatusInternalServerError)
		return
	}	
}