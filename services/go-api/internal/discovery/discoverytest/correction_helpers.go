package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"sync"
)

type CtxRecordingVocab struct {
	Mu               sync.Mutex
	FindClosestCalls int
	Queries          []string
	SawNoDeadline    bool
}

func (v *CtxRecordingVocab) Record(ctx context.Context) {
	if _, ok := ctx.Deadline(); !ok {
		v.SawNoDeadline = true
	}
}

func (v *CtxRecordingVocab) SuggestByPrefix(ctx context.Context, _ string, _ int) ([]domain.VocabularyEntry, error) {
	v.Mu.Lock()
	defer v.Mu.Unlock()
	v.Record(ctx)
	return nil, nil
}

func (v *CtxRecordingVocab) FindClosest(ctx context.Context, query string, _ int) ([]domain.VocabularyEntry, error) {
	v.Mu.Lock()
	defer v.Mu.Unlock()
	v.Record(ctx)
	v.FindClosestCalls++
	v.Queries = append(v.Queries, query)
	return nil, nil
}
