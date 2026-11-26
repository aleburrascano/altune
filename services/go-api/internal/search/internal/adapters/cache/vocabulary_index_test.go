package cache_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/cache"
	"context"
	"net"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

type pipelineRecorder struct {
	mu   sync.Mutex
	cmds []goredis.Cmder
}

func (r *pipelineRecorder) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (r *pipelineRecorder) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook { return next }

func (r *pipelineRecorder) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.cmds = append(r.cmds, cmds...)
		return nil
	}
}

func (r *pipelineRecorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.cmds)
}

func newRecordingVocabStore(t *testing.T) (*cache.RedisVocabularyStore, *pipelineRecorder) {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr: "127.0.0.1:1",
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			t.Fatalf("vocabulary index write must not dial")
			return nil, net.ErrClosed
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	rec := &pipelineRecorder{}
	client.AddHook(rec)
	return newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone), rec
}

func TestVocabularyStore_OversizedTerm_NotIndexed(t *testing.T) {
	ctx := context.Background()
	oversized := strings.Repeat("a", domain.MaxVocabularyTermRunes+1)

	store, rec := newRecordingVocabStore(t)
	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: oversized, Kind: domain.VocabKindQuery}); err != nil {
		t.Fatalf("Add(oversized): %v", err)
	}
	if err := store.BulkAdd(ctx, []domain.SpellingHintsEntry{{Term: oversized, Kind: domain.VocabKindTrack}}); err != nil {
		t.Fatalf("BulkAdd(oversized): %v", err)
	}
	if n := rec.count(); n != 0 {
		t.Fatalf("oversized term produced %d index commands, want 0", n)
	}

	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: "Kendrick", Kind: domain.VocabKindArtist}); err != nil {
		t.Fatalf("Add(normal): %v", err)
	}
	afterAdd := rec.count()
	if afterAdd == 0 {
		t.Fatalf("normal term was not indexed")
	}
	mixed := []domain.SpellingHintsEntry{{Term: oversized}, {Term: "Kendrick", Kind: domain.VocabKindArtist}}
	if err := store.BulkAdd(ctx, mixed); err != nil {
		t.Fatalf("BulkAdd(mixed): %v", err)
	}
	if got := rec.count() - afterAdd; got != afterAdd {
		t.Errorf("mixed BulkAdd wrote %d commands, want %d (normal entry only)", got, afterAdd)
	}
}
