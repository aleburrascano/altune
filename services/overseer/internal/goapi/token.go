// Package goapi is Overseer's read-only client for the Altune go-api public HTTP
// surface. Buckets observe the watched app through this package only; they never
// import go-api internal runtime packages (enforced by the guard spine test).
//
// The client authenticates as an operator principal by attaching a bearer token
// obtained from a single TokenSource — the one place token acquisition lives, so
// the SSE consumer leaf reuses this source rather than minting a second copy of
// the auth logic. Every exported method is a read (GET): there is no path here
// that writes, commands or mutates go-api, which realizes the observe-only spine
// invariant at the client boundary.
package goapi

import "context"

// TokenSource yields the operator JWT the client attaches to each go-api
// request. It is deliberately the sole seam for token acquisition: an
// implementation may return a fixed token, cache one, or refresh it on demand,
// and the client neither knows nor cares which. The SSE consumer leaf takes the
// same TokenSource, so operator identity is acquired in exactly one place.
type TokenSource interface {
	// Token returns the operator bearer token to present, or an error if none
	// can be obtained (which the caller surfaces rather than sending a request
	// with no credentials).
	Token(ctx context.Context) (string, error)
}

// StaticTokenSource serves a fixed operator token supplied at construction time
// (for example, injected from the environment at deploy). A refreshing source
// that satisfies TokenSource slots in later without the client changing.
type StaticTokenSource string

// Token returns the fixed token, or ErrNoToken when it is empty so the client
// fails closed instead of sending an unauthenticated request.
func (s StaticTokenSource) Token(context.Context) (string, error) {
	if s == "" {
		return "", ErrNoToken
	}
	return string(s), nil
}
