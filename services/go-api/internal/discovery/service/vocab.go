package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"log/slog"
	"time"
)

const (
	vocabIngestTop     = 5
	vocabIngestTimeout = 3 * time.Second
)

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

func (v *VocabularyIngestor) ingest(parentCtx context.Context, results []domain.SearchResult) {
	if v.vocabStore == nil || len(results) == 0 {
		return
	}
	entries := buildVocabEntries(results)

	v.bg.launch(parentCtx, "vocab.ingest", func(ctx context.Context) {
		ingestCtx, cancel := context.WithTimeout(ctx, vocabIngestTimeout)
		defer cancel()
		for _, e := range entries {
			if err := v.vocabStore.Add(ingestCtx, e); err != nil {
				slog.WarnContext(ingestCtx, "search.v2.vocab_ingest_failed",
					"kind", e.Kind, logging.SearchTextAttr(e.Term), "error", logging.ScrubSearchErr(err, e.Term))
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

func buildVocabEntries(results []domain.SearchResult) []domain.VocabularyEntry {
	limit := min(len(results), vocabIngestTop)
	var entries []domain.VocabularyEntry
	for _, r := range results[:limit] {
		entries = append(entries, resultVocabEntries(r)...)
	}
	return entries
}

func resultVocabEntries(r domain.SearchResult) []domain.VocabularyEntry {
	text := r.Title
	if r.Subtitle != "" {
		text = r.Title + " - " + r.Subtitle
	}
	entries := []domain.VocabularyEntry{newVocabEntry(text, resultKindToVocabKind(r.Kind), r.Popularity)}
	if r.Subtitle != "" && r.Kind == domain.ResultKindTrack {
		entries = append(entries, newVocabEntry(r.Subtitle, domain.VocabKindArtist, r.Popularity))
	}
	return entries
}

func newVocabEntry(term string, kind domain.VocabularyKind, popularity float64) domain.VocabularyEntry {
	return domain.VocabularyEntry{
		Term:       term,
		TermNorm:   textnorm.NormalizeForMatch(term),
		Kind:       kind,
		Popularity: int64(popularity),
	}
}
