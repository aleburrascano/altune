package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	vocabIngestTop     = 5
	vocabIngestTimeout = 3 * time.Second
)

type SpellingHintsIngestor struct {
	vocabStore ports.SpellingHintsStore
	bg         *background.Tracker
}

var ErrVocabularyCollaboratorRequired = errors.New("vocabulary services need every collaborator, pass the absent adapter to switch one off")

func NewVocabularyIngestor(vocabStore ports.SpellingHintsStore, bg *background.Tracker) (*SpellingHintsIngestor, error) {
	if vocabStore == nil || bg == nil {
		return nil, ErrVocabularyCollaboratorRequired
	}
	return &SpellingHintsIngestor{vocabStore: vocabStore, bg: bg}, nil
}

func (v *SpellingHintsIngestor) Ingest(parentCtx context.Context, results []domain.SearchResult) {
	if len(results) == 0 {
		return
	}
	entries := buildVocabEntries(results)

	v.bg.LaunchWithin(parentCtx, "vocab.ingest", vocabIngestTimeout, func(ctx context.Context) {
		for _, e := range entries {
			if err := v.vocabStore.Add(ctx, e); err != nil {
				slog.WarnContext(ctx, "search.v2.vocab_ingest_failed",
					"kind", e.Kind, logging.SearchTextAttr(e.Term), logging.KeyError, logging.ScrubSearchErr(err, e.Term))
			}
		}
	})
}

var vocabKindByResultKind = map[domain.ResultKind]domain.SpellingHintsKind{
	domain.ResultKindArtist: domain.VocabKindArtist,
	domain.ResultKindTrack:  domain.VocabKindTrack,
	domain.ResultKindAlbum:  domain.VocabKindAlbum,
}

func resultKindToVocabKind(k domain.ResultKind) domain.SpellingHintsKind {
	if vk, ok := vocabKindByResultKind[k]; ok {
		return vk
	}
	return domain.VocabKindQuery
}

func buildVocabEntries(results []domain.SearchResult) []domain.SpellingHintsEntry {
	limit := min(len(results), vocabIngestTop)
	var entries []domain.SpellingHintsEntry
	for _, r := range results[:limit] {
		entries = append(entries, resultVocabEntries(r)...)
	}
	return entries
}

func resultVocabEntries(r domain.SearchResult) []domain.SpellingHintsEntry {
	text := r.Title
	if r.Subtitle != "" {
		text = r.Title + " - " + r.Subtitle
	}
	entries := []domain.SpellingHintsEntry{newVocabEntry(text, resultKindToVocabKind(r.Kind), r.Popularity)}
	if r.Subtitle != "" && r.Kind == domain.ResultKindTrack {
		entries = append(entries, newVocabEntry(r.Subtitle, domain.VocabKindArtist, r.Popularity))
	}
	return entries
}

func newVocabEntry(term string, kind domain.SpellingHintsKind, popularity float64) domain.SpellingHintsEntry {
	return domain.SpellingHintsEntry{
		Term:       term,
		TermNorm:   textnorm.NormalizeForMatch(term),
		Kind:       kind,
		Popularity: int64(popularity),
	}
}
