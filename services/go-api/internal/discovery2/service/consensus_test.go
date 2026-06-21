package service

import (
	"context"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
)

// provider builds a consensus provider that returns the given album titles.
func provider(name string, albums ...string) ConsensusProvider {
	return ConsensusProvider{
		Name: name,
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			out := make([]domain.SearchResult, len(albums))
			for i, title := range albums {
				out[i] = domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, Subtitle: "Artist"}
			}
			return out, nil
		},
	}
}

func statusByTitle(albums []ConsensusAlbum) map[string]ConsensusStatus {
	m := make(map[string]ConsensusStatus, len(albums))
	for _, a := range albums {
		m[a.Album.Title] = a.Status
	}
	return m
}

type fakeMB struct {
	mbid          string
	confirmed     []string
	contamination map[string]bool
}

func (m *fakeMB) ResolveArtistIdentity(_ context.Context, _ string) (*ports.ArtistIdentity, error) {
	if m.mbid == "" {
		return nil, nil
	}
	return &ports.ArtistIdentity{MBID: m.mbid}, nil
}

func (m *fakeMB) ValidateArtistAlbums(_ context.Context, _ string, _ []domain.SearchResult) (*ports.AlbumValidationResult, error) {
	conf := make([]domain.SearchResult, len(m.confirmed))
	for i, t := range m.confirmed {
		conf[i] = domain.SearchResult{Title: t}
	}
	return &ports.AlbumValidationResult{Confirmed: conf}, nil
}

func (m *fakeMB) LookupAlbumArtist(_ context.Context, _, albumTitle string, _ domain.ArtistIdentityProfile) (domain.AlbumVerdict, string, error) {
	if m.contamination[textnorm.NormalizeForMatch(albumTitle)] {
		return domain.AlbumVerdictContamination, "", nil
	}
	return domain.AlbumVerdictUnknown, "", nil
}

func TestConsensus_ConfirmedAndUnconfirmed(t *testing.T) {
	svc := NewConsensusService([]ConsensusProvider{
		provider("lastfm", "Album A", "Album B"),
		provider("tidal", "Album A"),
	})
	got := svc.BuildConsensus(context.Background(), "Artist", nil)

	byTitle := statusByTitle(got)
	if byTitle["Album A"] != ConsensusConfirmed {
		t.Errorf("Album A (2 providers) = %v, want confirmed", byTitle["Album A"])
	}
	if byTitle["Album B"] != ConsensusUnconfirmed {
		t.Errorf("Album B (1 provider) = %v, want unconfirmed", byTitle["Album B"])
	}
}

func TestConsensus_DistinctAlbumsStaySeparate(t *testing.T) {
	// Canonical clustering: genuinely different album titles stay separate; the
	// parenthetical "(Deluxe)" is canonical noise and folds into "Scorpion".
	svc := NewConsensusService([]ConsensusProvider{
		provider("lastfm", "Scorpion", "Scorpion (Deluxe)", "Take Care"),
		provider("tidal", "Scorpion"),
	})
	got := svc.BuildConsensus(context.Background(), "Drake", nil)

	byTitle := statusByTitle(got)
	if _, ok := byTitle["Take Care"]; !ok {
		t.Error("expected the distinct album 'Take Care' to remain")
	}
	// "Scorpion" + "Scorpion (Deluxe)" + tidal "Scorpion" all normalize alike →
	// one confirmed cluster (3 provider hits).
	if byTitle["Scorpion"] != ConsensusConfirmed {
		t.Errorf("Scorpion = %v, want confirmed (deluxe folds in)", byTitle["Scorpion"])
	}
	if _, ok := byTitle["Scorpion (Deluxe)"]; ok {
		t.Error("'Scorpion (Deluxe)' should have folded into 'Scorpion', not stand alone")
	}
}

func TestConsensus_CacheSkipsProviderCalls(t *testing.T) {
	var calls int32
	p := ConsensusProvider{
		Name: "lastfm",
		Fetcher: func(_ context.Context, _ string) ([]domain.SearchResult, error) {
			atomic.AddInt32(&calls, 1)
			return []domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "X", Subtitle: "Artist"}}, nil
		},
	}
	svc := NewConsensusService([]ConsensusProvider{p})

	svc.BuildConsensus(context.Background(), "Artist", nil)
	svc.BuildConsensus(context.Background(), "Artist", nil)

	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("provider fetched %d times, want 1 (second served from cache)", n)
	}
}

func TestConsensus_DeterministicAcrossRuns(t *testing.T) {
	build := func() []ConsensusAlbum {
		svc := NewConsensusService([]ConsensusProvider{
			provider("lastfm", "A", "B", "C"),
			provider("tidal", "B", "D"),
			provider("itunes", "A", "C", "E"),
		})
		return svc.BuildConsensus(context.Background(), "Artist", nil)
	}

	first := build()
	for i := 0; i < 5; i++ {
		got := build()
		if len(got) != len(first) {
			t.Fatalf("run %d: len = %d, want %d", i, len(got), len(first))
		}
		for j := range got {
			if got[j].Album.Title != first[j].Album.Title || got[j].Status != first[j].Status {
				t.Fatalf("run %d position %d: got (%q,%v), want (%q,%v)",
					i, j, got[j].Album.Title, got[j].Status, first[j].Album.Title, first[j].Status)
			}
		}
	}
}

func TestConsensus_MBRejectsContaminationAndConfirms(t *testing.T) {
	mb := &fakeMB{
		mbid:          "mb1",
		confirmed:     []string{"Real Album"},
		contamination: map[string]bool{"fake album": true},
	}
	svc := NewConsensusService([]ConsensusProvider{
		provider("lastfm", "Real Album", "Fake Album"),
	}, WithMBAuthority(mb))

	got := svc.BuildConsensus(context.Background(), "Artist", nil)
	byTitle := statusByTitle(got)

	if byTitle["Real Album"] != ConsensusConfirmed {
		t.Errorf("Real Album = %v, want confirmed (MB-confirmed)", byTitle["Real Album"])
	}
	if byTitle["Fake Album"] != ConsensusRejected {
		t.Errorf("Fake Album = %v, want rejected (MB contamination)", byTitle["Fake Album"])
	}
}
