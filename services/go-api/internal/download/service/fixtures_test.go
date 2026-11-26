package service_test

import (
	"altune/go-api/internal/download"
	"context"
	"os"
	"path/filepath"
	"sync"
)

type resolver struct {
	identity download.RecordingIdentity
	err      error
	queries  []download.RecordingQuery
}

func (r *resolver) Resolve(_ context.Context, q download.RecordingQuery) (download.RecordingIdentity, error) {
	r.queries = append(r.queries, q)
	return r.identity, r.err
}

func resolvedTo(mbid string) *resolver {
	return &resolver{identity: download.RecordingIdentity{MBID: mbid, MBIDs: []string{mbid}}}
}

type identifier struct {
	mu    sync.Mutex
	match download.RecordingMatch
	err   error
	calls int
}

func (i *identifier) Identify(context.Context, string, float64) (download.RecordingMatch, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	return i.match, i.err
}

type storedAudio struct{}

func (storedAudio) Exists(context.Context, string) (bool, error) { return true, nil }

func (storedAudio) Store(context.Context, string, string) error { return nil }

func (storedAudio) Delete(context.Context, string) error { return nil }

func (storedAudio) CopyStored(_ context.Context, audioRef, dir string) (string, error) {
	path := filepath.Join(dir, "stored"+filepath.Ext(audioRef))
	return path, os.WriteFile(path, []byte("stored bytes"), 0o600)
}
