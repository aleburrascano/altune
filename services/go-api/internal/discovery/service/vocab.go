package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"time"
)

const (
	vocabIngestTop     = 5
	vocabIngestTimeout = 3 * time.Second
)

// VocabularyIngestor is the vocabulary-ingestion collaborator: it feeds the raw
// query and top results back into the learned VocabularyStore that backs
// correction and autocomplete, off the request path. Pulled off Service like
// FindRelatedService so the ingest shape can change without touching the
// orchestrator.
type VocabularyIngestor struct {
	vocabStore ports.VocabularyStore
	bg         *backgroundRunner
}

func newVocabularyIngestor(vocabStore ports.VocabularyStore, bg *backgroundRunner) *VocabularyIngestor {
	return &VocabularyIngestor{vocabStore: vocabStore, bg: bg}
}

func (v *VocabularyIngestor) ingest(parentCtx context.Context, rawQuery string, results []domain.SearchResult) {
	if v.vocabStore == nil || len(results) == 0 {
		return
	}
	entries := buildVocabEntries(rawQuery, results)

	v.bg.launch(parentCtx, "vocab.ingest", func(ctx context.Context) {
		ingestCtx, cancel := context.WithTimeout(ctx, vocabIngestTimeout)
		defer cancel()
		for _, e := range entries {
			if err := v.vocabStore.Add(ingestCtx, e); err != nil {
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
