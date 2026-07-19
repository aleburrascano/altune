package service

import (
	"context"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

const (
	// vocabIngestTop is how many top results feed the vocabulary per search (an
	// operational bound, like a page size).
	vocabIngestTop = 5
	// vocabIngestTimeout bounds the background ingest.
	vocabIngestTimeout = 3 * time.Second
)

// ingestVocabulary learns the query and its strong results into the vocabulary
// store, asynchronously and best-effort. This keeps Layer-0 intent detection and
// query correction improving from real traffic without blocking the request.
func (s *Service) ingestVocabulary(parentCtx context.Context, rawQuery string, results []domain.SearchResult) {
	if s.vocabStore == nil || len(results) == 0 {
		return
	}
	entries := buildVocabEntries(rawQuery, results)

	s.launchBackground(parentCtx, "vocab.ingest", func(ctx context.Context) {
		ingestCtx, cancel := context.WithTimeout(ctx, vocabIngestTimeout)
		defer cancel()
		for _, e := range entries {
			if err := s.vocabStore.Add(ingestCtx, e); err != nil {
				slog.WarnContext(ingestCtx, "search.v2.vocab_ingest_failed", "term", e.Term, "error", err)
			}
		}
	})
}

var vocabKindByResultKind = map[domain.ResultKind]domain.VocabularyKind{
	domain.ResultKindArtist: domain.VocabKindArtist,
	domain.ResultKindTrack:  domain.VocabKindTrack,
	domain.ResultKindAlbum:  domain.VocabKindAlbum,
}

func resultKindToVocabKind(k domain.ResultKind) domain.VocabularyKind {
	if vk, ok := vocabKindByResultKind[k]; ok {
		return vk
	}
	return domain.VocabKindQuery
}

func buildVocabEntries(rawQuery string, results []domain.SearchResult) []domain.VocabularyEntry {
	entries := []domain.VocabularyEntry{{
		Term:     rawQuery,
		TermNorm: textnorm.NormalizeForMatch(rawQuery),
		Kind:     domain.VocabKindQuery,
	}}

	limit := vocabIngestTop
	if len(results) < limit {
		limit = len(results)
	}
	for _, r := range results[:limit] {
		pop := r.Popularity
		text := r.Title
		if r.Subtitle != "" {
			text = r.Title + " - " + r.Subtitle
		}
		entries = append(entries, domain.VocabularyEntry{
			Term:       text,
			TermNorm:   textnorm.NormalizeForMatch(text),
			Kind:       resultKindToVocabKind(r.Kind),
			Popularity: int64(pop),
		})
		if r.Subtitle != "" && r.Kind == domain.ResultKindTrack {
			entries = append(entries, domain.VocabularyEntry{
				Term:       r.Subtitle,
				TermNorm:   textnorm.NormalizeForMatch(r.Subtitle),
				Kind:       domain.VocabKindArtist,
				Popularity: int64(pop),
			})
		}
	}
	return entries
}
