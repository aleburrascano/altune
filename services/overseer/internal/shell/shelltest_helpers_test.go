package shell_test

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"
)

const ownerID = "00000000-0000-0000-0000-000000000001"

const (
	ownerToken    = "valid-owner-token"
	nonOwnerToken = "valid-nonowner-token"
)

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

type panicBucket struct{}

func (panicBucket) Meta() core.Meta                                { return core.Meta{ID: "boom", Title: "Boom"} }
func (panicBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (panicBucket) Store([]core.Signal)                            {}
func (panicBucket) Snapshot() core.Snapshot                        { panic("snapshot blew up") }

type fixedRegistry struct{ buckets []core.Bucket }

func (f fixedRegistry) Buckets() []core.Bucket { return f.buckets }

func testStaticFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<!doctype html><title>Overseer</title>")},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}
}

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

func captureSlog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func logRecordsNamed(buf *bytes.Buffer, name string) []map[string]any {
	var found []map[string]any
	for _, line := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var rec map[string]any
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if rec["msg"] == name {
			found = append(found, rec)
		}
	}
	return found
}
