package middleware

import (
	"net/http"
	"time"

	"github.com/abneribeiro/internal/metrics"
	"github.com/go-chi/chi/v5"
	chimiddleware "github.com/go-chi/chi/v5/middleware"
)

// metricsEndpoint is excluded so the scrape does not instrument itself.
const metricsEndpoint = "/metrics"

func MetricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// chi's wrapper preserves Flusher/Hijacker/ReaderFrom, which a
		// hand-rolled struct silently drops.
		ww := chimiddleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		// RoutePattern is only populated once chi has matched, which is why
		// this runs after the handler. Labelling with r.URL.Path instead would
		// mint a new time series for every UUID a client ever sends.
		pattern := chi.RouteContext(r.Context()).RoutePattern()
		switch pattern {
		case metricsEndpoint:
			return
		case "":
			pattern = "unmatched"
		}

		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		metrics.RecordHTTPRequest(r.Method, pattern, status, time.Since(start))
	})
}
