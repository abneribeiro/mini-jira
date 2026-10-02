package server

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/abneribeiro/internal/task"
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
	r.Get("/", getUsers(db))

	r.Mount("/tasks", task.Routes(db))
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


type User struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	// Version is the optimistic-locking token, surfaced to clients as an ETag.
	// Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}




func getUsers(db *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const query = `
			SELECT id, name, email, created_at, updated_at
			FROM users
			ORDER BY id
		`

		rows, err := db.Query(r.Context(), query)
		if err != nil {
			http.Error(w, "failed to query users", http.StatusInternalServerError)
			return
		}
		defer rows.Close()

		users := make([]User, 0)

		for rows.Next() {
			var user User

			err := rows.Scan(
				&user.ID,
				&user.Name,
				&user.Email,
				&user.CreatedAt,
				&user.UpdatedAt,
			)
			if err != nil {
				http.Error(w, "failed to scan user", http.StatusInternalServerError)
				return
			}

			users = append(users, user)
		}

		if err := rows.Err(); err != nil {
			http.Error(w, "failed to read users", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(users); err != nil {
			return
		}
	}
}
