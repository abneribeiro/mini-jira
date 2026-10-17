package httpx

import (
	"encoding/json"
	"log/slog"
	"net/http"
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
