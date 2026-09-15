package domain

import (
	"testing"
)

func TestAlbumVerdict_String(t *testing.T) {
	tests := []struct {
		verdict AlbumVerdict
		want    string
	}{
		{AlbumVerdictUnknown, "unknown"},
		{AlbumVerdictConfirmed, "confirmed"},
		{AlbumVerdictContamination, "contamination"},
		{AlbumVerdictSuspect, "suspect"},
		{AlbumVerdict(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.verdict.String()
			if got != tt.want {
				t.Errorf("AlbumVerdict(%d).String() = %q, want %q", tt.verdict, got, tt.want)
			}
		})
	}
}

func TestNewArtistIdentityProfile(t *testing.T) {
	p := NewArtistIdentityProfile()

	if p.GenreCluster == nil {
		t.Fatal("GenreCluster should not be nil")
	}
	if p.KnownISRCRegistrants == nil {
		t.Fatal("KnownISRCRegistrants should not be nil")
	}
	if len(p.GenreCluster) != 0 {
		t.Errorf("GenreCluster should be empty, got %d", len(p.GenreCluster))
	}
	if len(p.KnownISRCRegistrants) != 0 {
		t.Errorf("KnownISRCRegistrants should be empty, got %d", len(p.KnownISRCRegistrants))
	}
}

func TestArtistIdentityProfile_AddGenre(t *testing.T) {
	p := NewArtistIdentityProfile()

	p.AddGenre("Rock")
	p.AddGenre("jazz")
	p.AddGenre("ROCK")

	if len(p.GenreCluster) != 2 {
		t.Errorf("GenreCluster should have 2 entries, got %d", len(p.GenreCluster))
	}
	if !p.GenreCluster["rock"] {
		t.Error("expected 'rock' in GenreCluster")
	}
	if !p.GenreCluster["jazz"] {
		t.Error("expected 'jazz' in GenreCluster")
	}
}

func TestArtistIdentityProfile_AddISRCRegistrant(t *testing.T) {
	p := NewArtistIdentityProfile()

	p.AddISRCRegistrant("J842")
	p.AddISRCRegistrant("ABC1")
	p.AddISRCRegistrant("J842")

	if len(p.KnownISRCRegistrants) != 2 {
		t.Errorf("KnownISRCRegistrants should have 2 entries, got %d", len(p.KnownISRCRegistrants))
	}
	if !p.KnownISRCRegistrants["J842"] {
		t.Error("expected 'J842' in KnownISRCRegistrants")
	}
	if !p.KnownISRCRegistrants["ABC1"] {
		t.Error("expected 'ABC1' in KnownISRCRegistrants")
	}
}

func TestArtistIdentityProfile_HasGenreOverlap(t *testing.T) {
	tests := []struct {
		name   string
		genres []string
		query  []string
		want   bool
	}{
		{
			name:   "overlap exists",
			genres: []string{"rock", "jazz"},
			query:  []string{"Jazz", "classical"},
			want:   true,
		},
		{
			name:   "no overlap",
			genres: []string{"rock", "jazz"},
			query:  []string{"classical", "electronic"},
			want:   false,
		},
		{
			name:   "empty cluster",
			genres: []string{},
			query:  []string{"rock"},
			want:   false,
		},
		{
			name:   "empty query",
			genres: []string{"rock"},
			query:  []string{},
			want:   false,
		},
		{
			name:   "case insensitive match",
			genres: []string{"Hip Hop"},
			query:  []string{"HIP HOP"},
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := NewArtistIdentityProfile()
			for _, g := range tt.genres {
				p.AddGenre(g)
			}
			got := p.HasGenreOverlap(tt.query)
			if got != tt.want {
				t.Errorf("HasGenreOverlap(%v) = %v, want %v", tt.query, got, tt.want)
			}
		})
	}
}
