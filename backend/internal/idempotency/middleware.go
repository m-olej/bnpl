package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/molej/bnpl/backend/internal/platform/httpkit"
)

const maxRequestBody = 1 << 20 // 1 MiB

type options struct {
	ttl          time.Duration
	captureLimit int
	logger       *slog.Logger
}

// Option configures the middleware.
type Option func(*options)

// WithTTL sets how long a key is retained (default 24h).
func WithTTL(ttl time.Duration) Option {
	return func(o *options) { o.ttl = ttl }
}

// WithLogger sets the logger used for internal failures.
func WithLogger(logger *slog.Logger) Option {
	return func(o *options) {
		if logger != nil {
			o.logger = logger
		}
	}
}

// Middleware enforces the Idempotency-Key header on mutating requests.
func Middleware(store Claimer, opts ...Option) httpkit.Middleware {
	o := options{ttl: 24 * time.Hour, captureLimit: 64 << 10, logger: slog.Default()}
	for _, fn := range opts {
		fn(&o)
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isMutating(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
			if key == "" {
				httpkit.WriteError(w, http.StatusBadRequest, "idempotency_key_required",
					"mutating requests require an Idempotency-Key header")
				return
			}

			body, ok := readBody(w, r)
			if !ok {
				return
			}

			endpoint := r.Method + " " + r.URL.Path
			result, err := store.Claim(r.Context(), key, endpoint, hashBody(body), o.ttl)
			if err != nil {
				o.logger.Error("idempotency claim failed", slog.String("error", err.Error()))
				httpkit.WriteError(w, http.StatusInternalServerError, "internal_error", "internal server error")
				return
			}

			switch result.Outcome {
			case OutcomeAcquired:
				rec := newCaptureWriter(w, o.captureLimit)
				next.ServeHTTP(rec, r)
				if err := store.Complete(r.Context(), key, endpoint, rec.status, rec.body()); err != nil {
					o.logger.Error("idempotency completion failed", slog.String("error", err.Error()))
				}

			case OutcomeReplay:
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.Header().Set("Idempotent-Replay", "true")
				w.WriteHeader(result.Replay.StatusCode)
				_, _ = w.Write(result.Replay.Body)

			case OutcomeInFlight:
				w.Header().Set("Retry-After", "2")
				httpkit.WriteError(w, http.StatusConflict, "idempotency_in_flight",
					"a request with this Idempotency-Key is still being processed")

			case OutcomeMismatch:
				httpkit.WriteError(w, http.StatusUnprocessableEntity, "idempotency_key_reuse",
					"Idempotency-Key was reused with a different request payload")
			}
		})
	}
}

func readBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	if r.Body == nil {
		return nil, true
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBody+1))
	_ = r.Body.Close()
	if err != nil {
		httpkit.WriteError(w, http.StatusBadRequest, "invalid_body", "could not read request body")
		return nil, false
	}
	if len(body) > maxRequestBody {
		httpkit.WriteError(w, http.StatusRequestEntityTooLarge, "payload_too_large", "request body exceeds 1 MiB")
		return nil, false
	}
	// Replace the body so downstream handlers can read it again.
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, true
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func hashBody(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// captureWriter tees the response body so it can be cached for replay.
type captureWriter struct {
	http.ResponseWriter
	status int
	wrote  bool
	buf    bytes.Buffer
	limit  int
}

func newCaptureWriter(w http.ResponseWriter, limit int) *captureWriter {
	return &captureWriter{ResponseWriter: w, status: http.StatusOK, limit: limit}
}

func (c *captureWriter) WriteHeader(code int) {
	if !c.wrote {
		c.status = code
		c.wrote = true
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if !c.wrote {
		c.WriteHeader(http.StatusOK)
	}
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(b) <= remaining {
			c.buf.Write(b)
		} else {
			c.buf.Write(b[:remaining])
		}
	}
	return c.ResponseWriter.Write(b)
}

func (c *captureWriter) body() []byte { return c.buf.Bytes() }
