package unreleased

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"testing"
)

func TestCoverage_BuildLogsHowManySongsArePlayableLinkOutOrSilent(t *testing.T) {
	logs := captureLogs(t)
	list := songs("A", "B", "C", "D")
	for i := range list[:3] {
		list[i].CoverURL = "https://images.genius.com/own.jpg"
	}
	list[2].OpenURL = "https://mega.nz/file/x"
	list[2].OpenHost = "mega.nz"
	link := []domain.AudioLink{{Provider: domain.AudioProviderYouTube, ExternalID: "v", URL: "https://youtu.be/v"}}
	genius := &discoverytest.GeniusSongCatalog{
		ArtistFound: true,
		Songs:       list,
		DetailByID:  map[int]domain.GeniusSongDetail{1: {AudioLinks: link}, 2: {AudioLinks: link}},
	}
	svc := NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, Config{MaxPages: 1, MaxDetails: 2, DetailConcurrency: 2})
	if _, err := svc.List(context.Background(), domain.ProviderDeezer, "27", "Drake", nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	out := logs.String()
	if n := strings.Count(out, "unreleased.coverage"); n != 1 {
		t.Fatalf("coverage events = %d, want 1: %s", n, out)
	}
	for _, want := range []string{"provider=deezer", "external_id=27", "artist=Drake", "total=4", "playable=2", "link_out=1", "no_audio=1", "no_cover=1", "mega.nz:1"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}

func TestCoverage_StoreRebuildLogsOneEvent(t *testing.T) {
	logs := captureLogs(t)
	store := &discoverytest.UnreleasedStore{}
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	if _, err := storeService(genius, store).Rebuild(context.Background(), drakeKey, "Drake", nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	if n := strings.Count(logs.String(), "unreleased.coverage"); n != 1 {
		t.Errorf("coverage events = %d, want 1", n)
	}
}

func TestCoverage_EventCarriesTheTotalNumberOfVersions(t *testing.T) {
	logs := captureLogs(t)
	list := songs("A", "B")
	list[0].Versions = []domain.UnreleasedVersion{{Title: "A [V1]"}, {Title: "A [V2]"}, {Title: "A [V3]"}}
	list[1].Versions = []domain.UnreleasedVersion{{Title: "B [V1]"}, {Title: "B (Demo)"}}
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: list}
	if _, err := storeService(genius, &discoverytest.UnreleasedStore{}).Rebuild(context.Background(), drakeKey, "Drake", nil); err != nil {
		t.Fatalf("Rebuild: %v", err)
	}
	out := logs.String()
	for _, want := range []string{"total=2", "versions=5"} {
		if !strings.Contains(out, want) {
			t.Errorf("log missing %q: %s", want, out)
		}
	}
}
