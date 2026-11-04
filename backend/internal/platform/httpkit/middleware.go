package httpkit

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Middleware wraps an http.Handler.
type Middleware func(http.Handler) http.Handler

// Chain applies middleware so that the first argument is the outermost layer.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}
	return h
}

// RequestID assigns a trace id (from X-Request-Id or a new UUID) to the request.
func RequestID() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id := strings.TrimSpace(r.Header.Get("X-Request-Id"))
			if id == "" {
				id = uuid.NewString()
			}
			w.Header().Set("X-Request-Id", id)
			next.ServeHTTP(w, r.WithContext(WithRequestID(r.Context(), id)))
		})
	}
}

// Recoverer converts panics into 500 responses and logs them.
func Recoverer(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Error("panic recovered",
						slog.Any("panic", rec),
						slog.String("trace_id", RequestIDFromContext(r.Context())),
					)
					WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// Logging emits one structured log line per request.
func Logging(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rec, r)

			attrs := []slog.Attr{
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.String("trace_id", RequestIDFromContext(r.Context())),
			}
			if actor, ok := ActorFromContext(r.Context()); ok {
				attrs = append(attrs, slog.String("user_id", actor.ID), slog.String("actor_type", actor.Type))
			}
			if key := r.Header.Get("Idempotency-Key"); key != "" {
				attrs = append(attrs, slog.String("idempotency_key", key))
			}
			logger.LogAttrs(r.Context(), slog.LevelInfo, "http_request", attrs...)
		})
	}
}

// Auth is a placeholder identity layer: it requires X-Actor-Type and
// X-Actor-Id headers and stores the resulting actor in the context. A real
// deployment would replace this with token verification.
func Auth() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Documentation endpoints are public; only the API is guarded.
			if !strings.HasPrefix(r.URL.Path, "/api/") {
				next.ServeHTTP(w, r)
				return
			}

			actorType := strings.ToUpper(strings.TrimSpace(r.Header.Get("X-Actor-Type")))
			actorID := strings.TrimSpace(r.Header.Get("X-Actor-Id"))

			switch actorType {
			case "CUSTOMER", "MERCHANT", "ADMIN", "SYSTEM":
			default:
				WriteError(w, http.StatusUnauthorized, "unauthenticated",
					"missing or invalid X-Actor-Type header")
				return
			}
			if actorID == "" {
				WriteError(w, http.StatusUnauthorized, "unauthenticated",
					"missing X-Actor-Id header")
				return
			}
			next.ServeHTTP(w, r.WithContext(withActor(r.Context(), Actor{ID: actorID, Type: actorType})))
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if !s.wrote {
		s.WriteHeader(http.StatusOK)
	}
	return s.ResponseWriter.Write(b)
}
