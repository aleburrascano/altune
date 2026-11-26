package musicbrainz_test

import (
	"altune/go-api/internal/search/domain"
	"fmt"
	"strings"
	"testing"
)

type credit struct{ name, join, mbid string }

func recordingCreditedTo(credits ...credit) string {
	refs := make([]string, 0, len(credits))
	for _, c := range credits {
		artist := ""
		if c.mbid != "" {
			artist = fmt.Sprintf(`, "artist": {"id": %q, "name": %q}`, c.mbid, c.name)
		}
		refs = append(refs, fmt.Sprintf(`{"name": %q, "joinphrase": %q%s}`, c.name, c.join, artist))
	}
	return `{"id": "rec-1", "title": "Song", "artist-credit": [` + strings.Join(refs, ",") + `]}`
}

func featuredOn(t *testing.T, credits ...credit) []domain.FeaturedArtist {
	t.Helper()
	results := trackSearchResults(t, recordingCreditedTo(credits...))
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1 recording", len(results))
	}
	return domain.FeaturedArtistsFromExtras(results[0].Extras)
}

func TestMusicBrainzAdapter_SearchStructured_featuredArtistsFollowTheFeatJoinPhrase(t *testing.T) {
	tests := []struct {
		name    string
		credits []credit
		want    []string
	}{
		{"no featured, solo", []credit{{"Drake", "", "d"}}, nil},
		{"single feat", []credit{{"Drake", " feat. ", "d"}, {"Rihanna", "", "r"}}, []string{"Rihanna"}},
		{
			"feat group carries to all after boundary",
			[]credit{{"Main", " feat. ", "m"}, {"A", " & ", "a"}, {"B", "", "b"}},
			[]string{"A", "B"},
		},
		{"collaboration (& not feat) yields none", []credit{{"A", " & ", "a"}, {"B", "", "b"}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := featuredOn(t, tt.credits...)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d featured, want %d (%v)", len(got), len(tt.want), got)
			}
			for i, w := range tt.want {
				if got[i].Name != w || got[i].Role != domain.RoleFeatured {
					t.Errorf("featured[%d] = %+v, want %q as featured", i, got[i], w)
				}
			}
		})
	}
}

func TestMusicBrainzAdapter_SearchStructured_featuredArtistCarriesMBID(t *testing.T) {
	got := featuredOn(t, credit{"Drake", " feat. ", "d"}, credit{"Rihanna", "", "rih-mbid"})
	if len(got) != 1 || got[0].MBID != "rih-mbid" {
		t.Fatalf("expected featured with MBID rih-mbid, got %+v", got)
	}
}
