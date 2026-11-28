package service_test

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/songfields"
	"context"
	"os"
	"path/filepath"
	"sync"
)

type resolver struct {
	identity ports.RecordingIdentity
	err      error
	queries  []songfields.RecordingIdentity
}

func (r *resolver) Resolve(_ context.Context, q songfields.RecordingIdentity) (ports.RecordingIdentity, error) {
	r.queries = append(r.queries, q)
	return r.identity, r.err
}

func resolvedTo(mbid string) *resolver {
	return &resolver{identity: ports.RecordingIdentity{MBID: mbid, MBIDs: []string{mbid}}}
}

type identifier struct {
	mu    sync.Mutex
	match ports.RecordingMatch
	err   error
	calls int
}

func (i *identifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
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
