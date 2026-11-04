// Package httpkit provides shared HTTP middleware, response helpers and error
// mapping for the BNPL services.
package httpkit

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	actorKey
)

// Actor is the placeholder caller identity derived from request headers.
type Actor struct {
	ID   string
	Type string
}

// WithRequestID stores the request/trace id in the context.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

// RequestIDFromContext returns the request id, or "" when absent.
func RequestIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(requestIDKey).(string); ok {
		return id
	}
	return ""
}

func withActor(ctx context.Context, actor Actor) context.Context {
	return context.WithValue(ctx, actorKey, actor)
}

// ActorFromContext returns the caller identity, if one was established.
func ActorFromContext(ctx context.Context) (Actor, bool) {
	actor, ok := ctx.Value(actorKey).(Actor)
	return actor, ok
}
