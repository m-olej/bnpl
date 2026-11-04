package idempotency

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/molej/bnpl/backend/internal/platform/httpkit"
)

type completion struct {
	key      string
	endpoint string
	status   int
	body     string
}

type fakeStore struct {
	result      ClaimResult
	claimErr    error
	completions []completion
	claimCalls  int
	lastHash    string
}

func (f *fakeStore) Claim(_ context.Context, _, _, requestHash string, _ time.Duration) (ClaimResult, error) {
	f.claimCalls++
	f.lastHash = requestHash
	return f.result, f.claimErr
}

func (f *fakeStore) Complete(_ context.Context, key, endpoint string, statusCode int, body []byte) error {
	f.completions = append(f.completions, completion{key: key, endpoint: endpoint, status: statusCode, body: string(body)})
	return nil
}

func okHandler(called *bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		*called = true
		httpkit.WriteJSON(w, http.StatusCreated, map[string]string{"status": "created"})
	})
}

func TestMiddleware(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		method       string
		key          string
		result       ClaimResult
		wantStatus   int
		wantCalled   bool
		wantReplay   bool
		wantComplete int
	}{
		{
			name:         "acquired runs handler and completes",
			method:       http.MethodPost,
			key:          "key-1",
			result:       ClaimResult{Outcome: OutcomeAcquired},
			wantStatus:   http.StatusCreated,
			wantCalled:   true,
			wantComplete: 1,
		},
		{
			name:       "replay returns cached response",
			method:     http.MethodPost,
			key:        "key-1",
			result:     ClaimResult{Outcome: OutcomeReplay, Replay: &Replay{StatusCode: http.StatusCreated, Body: []byte(`{"cached":true}`)}},
			wantStatus: http.StatusCreated,
			wantCalled: false,
			wantReplay: true,
		},
		{
			name:       "in flight returns 409",
			method:     http.MethodPost,
			key:        "key-1",
			result:     ClaimResult{Outcome: OutcomeInFlight},
			wantStatus: http.StatusConflict,
			wantCalled: false,
		},
		{
			name:       "mismatch returns 422",
			method:     http.MethodPost,
			key:        "key-1",
			result:     ClaimResult{Outcome: OutcomeMismatch},
			wantStatus: http.StatusUnprocessableEntity,
			wantCalled: false,
		},
		{
			name:       "missing key returns 400",
			method:     http.MethodPost,
			key:        "",
			wantStatus: http.StatusBadRequest,
			wantCalled: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			store := &fakeStore{result: tt.result}
			var called bool
			handler := httpkit.Chain(okHandler(&called),
				httpkit.RequestID(),
				Middleware(store),
			)

			req := httptest.NewRequest(tt.method, "/api/v1/checkout/authorize", strings.NewReader(`{"a":1}`))
			if tt.key != "" {
				req.Header.Set("Idempotency-Key", tt.key)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body=%s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if called != tt.wantCalled {
				t.Fatalf("handler called = %v, want %v", called, tt.wantCalled)
			}
			if tt.wantReplay && rec.Header().Get("Idempotent-Replay") != "true" {
				t.Errorf("missing Idempotent-Replay header")
			}
			if len(store.completions) != tt.wantComplete {
				t.Fatalf("completions = %d, want %d", len(store.completions), tt.wantComplete)
			}
			if tt.wantComplete == 1 {
				got := store.completions[0]
				if got.status != http.StatusCreated {
					t.Errorf("completed status = %d, want %d", got.status, http.StatusCreated)
				}
				if !strings.Contains(got.body, `"status":"created"`) {
					t.Errorf("completed body = %q, want to contain status created", got.body)
				}
			}
		})
	}
}

func TestMiddlewareSkipsReadRequests(t *testing.T) {
	t.Parallel()

	store := &fakeStore{result: ClaimResult{Outcome: OutcomeInFlight}}
	var called bool
	handler := Middleware(store)(okHandler(&called))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/abc", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("GET handler was not called")
	}
	if store.claimCalls != 0 {
		t.Fatalf("claimCalls = %d, want 0 for read request", store.claimCalls)
	}
}

func TestMiddlewareClaimErrorReturns500(t *testing.T) {
	t.Parallel()

	store := &fakeStore{claimErr: errors.New("db down")}
	var called bool
	handler := Middleware(store)(okHandler(&called))

	req := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{}`))
	req.Header.Set("Idempotency-Key", "k")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if called {
		t.Fatal("handler should not run when claim fails")
	}
}
