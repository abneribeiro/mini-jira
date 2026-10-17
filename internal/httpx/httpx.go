package httpx

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const MaxBodyBytes = 1 << 20 // 1 MiB

// Stable, client-facing error codes. Entity-specific codes (task_not_found,
// email_already_exists, ...) are passed by the owning package.
const (
	CodeInvalidRequest   = "invalid_request"
	CodeInvalidID        = "invalid_id"
	CodeValidation       = "validation_failed"
	CodeUnauthorized     = "unauthorized"
	CodeForbidden        = "forbidden"
	CodeNotFound         = "not_found"
	CodeMethodNotAllowed = "method_not_allowed"
	CodeInternal         = "internal_error"
)

type errorEnvelope struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON marshals before touching the ResponseWriter, so a failure to
// encode cannot leave a truncated body behind an already-sent success status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Error("marshal response body", "error", err)
		WriteError(w, http.StatusInternalServerError, CodeInternal, "internal server error")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("write response body", "error", err)
	}
}

// WriteError emits {"error":{"code":...,"message":...}}. The message is always
// marshalled, never interpolated, so it stays valid JSON whatever it contains.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	body, err := json.Marshal(errorEnvelope{errorDetail{Code: code, Message: message}})
	if err != nil {
		slog.Error("marshal error body", "error", err)
		body = []byte(`{"error":{"code":"internal_error","message":"internal server error"}}`)
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(body); err != nil {
		slog.Error("write error body", "error", err)
	}
}

// WriteNoContent sends 204 without a body or a Content-Type.
func WriteNoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

// DecodeJSON caps and closes the body, so callers cannot forget either.
func DecodeJSON(w http.ResponseWriter, r *http.Request, v any) error {
	r.Body = http.MaxBytesReader(w, r.Body, MaxBodyBytes)
	defer r.Body.Close()

	return json.NewDecoder(r.Body).Decode(v)
}

// URLParamUUID parses the named chi path parameter as a UUID. On failure it
// writes 400 invalid_id and reports false, so handlers can simply return.
func URLParamUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		WriteError(w, http.StatusBadRequest, CodeInvalidID, "invalid ID: must be a valid UUID")
		return uuid.Nil, false
	}

	return id, true
}

// ParsePagination reads limit and offset from the query string. It only
// rejects values that are not integers; each domain's service owns the range
// rules and wraps the error in its own validation sentinel.
func ParsePagination(r *http.Request, defaultLimit int) (limit, offset int, err error) {
	q := r.URL.Query()
	limit = defaultLimit

	if raw := q.Get("limit"); raw != "" {
		if limit, err = strconv.Atoi(raw); err != nil {
			return 0, 0, errors.New("limit must be an integer")
		}
	}

	if raw := q.Get("offset"); raw != "" {
		if offset, err = strconv.Atoi(raw); err != nil {
			return 0, 0, errors.New("offset must be an integer")
		}
	}

	return limit, offset, nil
}
