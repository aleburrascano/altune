package discoverytest

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type FakeVocabularyStore struct {
	BulkAdded         []domain.VocabularyEntry
	Err               error
	SuggestByPrefixFn func(prefix string, limit int) ([]domain.VocabularyEntry, error)
	FindClosestFn     func(query string, limit int) ([]domain.VocabularyEntry, error)
	AddFn             func(entry domain.VocabularyEntry) error
	SuggestCalls      int
	FindClosestCalls  int
	TrimCalls         int
	TrimmedTo         int
}

func (f *FakeVocabularyStore) Trim(_ context.Context, maxEntries int) error {
	f.TrimCalls++
	f.TrimmedTo = maxEntries
	return nil
}

func (f *FakeVocabularyStore) Add(_ context.Context, entry domain.VocabularyEntry) error {
	if f.AddFn != nil {
		return f.AddFn(entry)
	}
	return nil
}

func (f *FakeVocabularyStore) BulkAdd(_ context.Context, entries []domain.VocabularyEntry) error {
	f.BulkAdded = append(f.BulkAdded, entries...)
	return f.Err
}

func (f *FakeVocabularyStore) SuggestByPrefix(_ context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error) {
	f.SuggestCalls++
	if f.SuggestByPrefixFn != nil {
		return f.SuggestByPrefixFn(prefix, limit)
	}
	return nil, nil
}

func (f *FakeVocabularyStore) FindClosest(_ context.Context, query string, limit int) ([]domain.VocabularyEntry, error) {
	f.FindClosestCalls++
	if f.FindClosestFn != nil {
		return f.FindClosestFn(query, limit)
	}
	return nil, nil
}
