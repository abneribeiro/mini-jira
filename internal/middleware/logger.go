package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// RequestLogger replaces chi's middleware.Logger so request lines are
// structured and carry the request ID, instead of being a third, unstructured
// logging stack alongside slog.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		level := slog.LevelInfo
		switch {
		case status >= 500:
			level = slog.LevelError
		case status >= 400:
			level = slog.LevelWarn
		}

		slog.Log(r.Context(), level, "http request",
			"method", r.Method,
			"route", chi.RouteContext(r.Context()).RoutePattern(),
			"path", r.URL.Path,
			"status", status,
			"bytes", ww.BytesWritten(),
			"duration_ms", float64(time.Since(start).Microseconds())/1000,
			"request_id", chimiddleware.GetReqID(r.Context()),
			"remote_addr", r.RemoteAddr,
		)
	})
}
