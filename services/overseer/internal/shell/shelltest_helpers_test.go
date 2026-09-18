package shell_test

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing/fstest"
	"time"
)

// ownerID is the single allowlisted owner subject used across the shell tests.
const ownerID = "00000000-0000-0000-0000-000000000001"

// ownerToken and nonOwnerToken are opaque strings the fakeVerifier maps to
// subjects; they stand in for real Supabase JWTs so the guard's 401/403 paths are
// exercised without minting real signatures (the signature verification itself is
// covered by the authn package tests).
const (
	ownerToken    = "valid-owner-token"
	nonOwnerToken = "valid-nonowner-token"
)

// fakeVerifier is a controllable shell.Verifier: it maps a known token string to a
// subject and rejects everything else, so a test drives the missing/invalid (401)
// and non-owner (403) paths deterministically.
type fakeVerifier struct {
	subjects map[string]string
}

func newFakeVerifier() fakeVerifier {
	return fakeVerifier{subjects: map[string]string{
		ownerToken:    ownerID,
		nonOwnerToken: "99999999-9999-9999-9999-999999999999",
	}}
}

func (v fakeVerifier) Verify(_ context.Context, token string) (authn.Claims, error) {
	if sub, ok := v.subjects[token]; ok {
		return authn.Claims{Subject: sub}, nil
	}
	return authn.Claims{}, errors.New("invalid token")
}

// stubBucket is a fixed-snapshot bucket used to assert API shape and ordering.
type stubBucket struct {
	id    string
	state core.State
}

func (s stubBucket) Meta() core.Meta                                { return core.Meta{ID: s.id, Title: s.id} }
func (s stubBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (s stubBucket) Store([]core.Signal)                            {}
func (s stubBucket) Snapshot() core.Snapshot {
	return core.Snapshot{ID: s.id, Title: s.id, State: s.state, Data: json.RawMessage(`{"ok":true}`)}
}

// panicBucket panics on Snapshot to prove containment.
type panicBucket struct{}

func (panicBucket) Meta() core.Meta                                { return core.Meta{ID: "boom", Title: "Boom"} }
func (panicBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (panicBucket) Store([]core.Signal)                            {}
func (panicBucket) Snapshot() core.Snapshot                        { panic("snapshot blew up") }

type fixedRegistry struct{ buckets []core.Bucket }

func (f fixedRegistry) Buckets() []core.Bucket { return f.buckets }

// testStaticFS is a minimal embedded-SPA stand-in.
func testStaticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Overseer</title>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

// newServer builds a shell handler wired like production: a verifier, the owner
// allowlist, a static SPA, and public client config. Extra options let a test wire
// the collect-loop liveness /health reports.
func newServer(reg shell.Registry, opts ...shell.Option) http.Handler {
	return shell.NewHandler(reg,
		append([]shell.Option{
			shell.WithVerifier(newFakeVerifier()),
			shell.WithOwnerUserID(ownerID),
			shell.WithStaticFS(testStaticFS()),
			shell.WithClientConfig(shell.ClientConfig{SupabaseURL: "https://x.supabase.co", SupabaseAnonKey: "anon-key"}),
			shell.WithStreamInterval(10 * time.Millisecond),
		}, opts...)...,
	).Router()
}

// fixedCollectStatus wires /health to a known loop state.
func fixedCollectStatus(status shell.CollectStatus) shell.Option {
	return shell.WithCollectStatus(func() shell.CollectStatus { return status })
}

func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func withOwner(r *http.Request) *http.Request {
	r.Header.Set("Authorization", "Bearer "+ownerToken)
	return r
}
