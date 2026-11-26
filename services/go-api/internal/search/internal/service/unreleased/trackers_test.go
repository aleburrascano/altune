package unreleased_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
)

func pixeldrainLink(id string) search.AudioLink {
	return search.AudioLink{Provider: "pixeldrain", ExternalID: id, URL: "https://pixeldrain.com/u/" + id}
}

func trackerSong(title, id string) search.UnreleasedSong {
	return search.UnreleasedSong{Title: title, Quality: "High Quality", TrackerKey: "ken:" + id, AudioLinks: []search.AudioLink{pixeldrainLink(id)}}
}

func listWith(t *testing.T, genius *search.FakeGeniusSongCatalog, trackers *search.TrackerSongs, cache search.UnreleasedCache) ([]search.UnreleasedSong, error) {
	t.Helper()
	if cache == nil {
		cache = search.NoUnreleasedCache{}
	}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(geniusPort(genius), &search.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(cache, searchtest.UnreleasedTrackers(trackers))))
	return svc.List(context.Background(), search.ProviderDeezer, "1", "Ken Carson", nil)
}

func geniusPort(g *search.FakeGeniusSongCatalog) search.GeniusSongCatalog {
	if g == nil {
		return search.NoUnreleasedGenius{}
	}
	return g
}

func TestService_List_TrackerSongMergesIntoTheGeniusSongWithTheSameTitle(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Loot")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("⭐ Loot", "lootaaaa")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v, want one merged item", titlesOf(got), err)
	}
	if got[0].GeniusID != 1 || got[0].Quality != "High Quality" || !slices.Contains(got[0].AudioLinks, pixeldrainLink("lootaaaa")) {
		t.Errorf("merged = %+v, want genius id kept plus quality and the pixeldrain link", got[0])
	}
}

func TestService_List_TrackerSongNeverMergesIntoAnotherVersion(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Dream", "Dream [V1]")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Dream [V2]", "dreamv2a")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Dream", "Dream [V1]", "Dream [V2]"}
	if len(got) != len(want) || !slices.Equal(titlesOf(got), want) {
		t.Fatalf("titles = %v, want %v", titlesOf(got), want)
	}
	if got[0].Quality != "" || got[1].Quality != "" || got[2].GeniusID != 0 {
		t.Errorf("only the V2 item is tracker-only: %+v", got)
	}
}

func TestService_List_TrackerSongsDoNotUseUpDetailLookups(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("A")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("B", "bbbbbbbb"), trackerSong("C", "cccccccc")}}
	if _, err := listWith(t, genius, trackers, nil); err != nil {
		t.Fatal(err)
	}
	if got := genius.DetailLookups(); !slices.Equal(got, []int{1}) {
		t.Errorf("detail lookups = %v, want only the genius song", got)
	}
}

func TestService_List_ReleasedTrackerSongIsHidden(t *testing.T) {
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Magnolia", "magnolia"), trackerSong("Dream [V2]", "dreamv2a")}}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{Titles: []string{"Magnolia"}}, cachedConfig, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedTrackers(trackers))))
	got, err := svc.List(context.Background(), search.ProviderDeezer, "1", "Ken Carson", nil)
	if err != nil || !slices.Equal(titlesOf(got), []string{"Dream [V2]"}) {
		t.Errorf("got %v, err %v, want only the unreleased tracker song", titlesOf(got), err)
	}
}

func versionedTrackerSong(title string, versions ...search.UnreleasedVersion) search.UnreleasedSong {
	return search.UnreleasedSong{Title: title, TrackerKey: "ken:" + title, Versions: versions}
}

func TestService_List_ReleasedSongKeepsItsTaggedTrackerVersions(t *testing.T) {
	sportsCar := versionedTrackerSong("Sports Car",
		search.UnreleasedVersion{Title: "Sports Car", Quality: "Lossless"},
		search.UnreleasedVersion{Title: "Sports Car [V3]", Label: "V3", Quality: "High Quality"})
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{sportsCar, trackerSong("Magnolia", "magnolia")}}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{Titles: []string{"Sports Car", "Magnolia"}}, cachedConfig, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedTrackers(trackers))))
	got, err := svc.List(context.Background(), search.ProviderDeezer, "1", "Ken Carson", nil)
	if err != nil || len(got) != 1 || got[0].Title != "Sports Car" {
		t.Fatalf("got %v, err %v, want only Sports Car", titlesOf(got), err)
	}
	song := got[0]
	if len(song.Versions) != 1 || song.Versions[0].Title != "Sports Car [V3]" || song.Quality != "High Quality" {
		t.Errorf("song = %+v, want only the V3 version and its quality", song)
	}
}

func TestService_List_CatalogMatchDropsASongWhoseVersionsAreAllUntagged(t *testing.T) {
	song := versionedTrackerSong("Sports Car", search.UnreleasedVersion{Title: "Sports Car"})
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{song}}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{Titles: []string{"Sports Car"}}, cachedConfig, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedTrackers(trackers))))
	got, err := svc.List(context.Background(), search.ProviderDeezer, "1", "Ken Carson", nil)
	if err != nil || len(got) != 0 {
		t.Errorf("got %v, err %v, want nothing", titlesOf(got), err)
	}
}

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestService_List_TrackerFailureKeepsTheGeniusListAndGetsTheShortTTL(t *testing.T) {
	logs := captureLogs(t)
	cache := newMemoryCache()
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Leak")}
	got, err := listWith(t, genius, &search.TrackerSongs{Err: errors.New("sheets 503")}, cache)
	if err != nil || !slices.Equal(titlesOf(got), []string{"Leak"}) {
		t.Fatalf("got %v, err %v, want the genius-only list", titlesOf(got), err)
	}
	if ttl := cache.ttls["1|ken carson"]; ttl != shortTTL {
		t.Errorf("ttl = %v, want %v", ttl, shortTTL)
	}
	if !strings.Contains(logs.String(), "trackers.fetch_failed") {
		t.Errorf("log %q lacks trackers.fetch_failed", logs.String())
	}
}

func TestService_List_GeniusFailureKeepsTheTrackerList(t *testing.T) {
	cache := newMemoryCache()
	genius := &search.FakeGeniusSongCatalog{Err: errors.New("genius 401")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Dream [V2]", "dreamv2a")}}
	got, err := listWith(t, genius, trackers, cache)
	if err != nil || !slices.Equal(titlesOf(got), []string{"Dream [V2]"}) {
		t.Fatalf("got %v, err %v, want the tracker-only list", titlesOf(got), err)
	}
	if ttl := cache.ttls["1|ken carson"]; ttl != shortTTL {
		t.Errorf("ttl = %v, want %v after a genius outage", ttl, shortTTL)
	}
}

func TestService_List_GeniusNotFoundKeepsTheTrackerListAtTheLongTTL(t *testing.T) {
	cache := newMemoryCache()
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Dream [V2]", "dreamv2a")}}
	got, err := listWith(t, &search.FakeGeniusSongCatalog{ArtistFound: false}, trackers, cache)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v", titlesOf(got), err)
	}
	if ttl := cache.ttls["1|ken carson"]; ttl != longTTL {
		t.Errorf("ttl = %v, want %v for a real not-found", ttl, longTTL)
	}
}

func TestService_List_NoGeniusPortYieldsTrackerSongsWithAShortTTL(t *testing.T) {
	cache := newMemoryCache()
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Dream [V2]", "dreamv2a")}}
	got, err := listWith(t, nil, trackers, cache)
	if err != nil || !slices.Equal(titlesOf(got), []string{"Dream [V2]"}) {
		t.Fatalf("got %v, err %v, want the tracker-only list", titlesOf(got), err)
	}
	if ttl := cache.ttls["1|ken carson"]; ttl != shortTTL {
		t.Errorf("ttl = %v, want %v with no genius port", ttl, shortTTL)
	}
}

func TestService_List_BothSourcesFailingIsAnError(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{Err: errors.New("genius down")}
	if _, err := listWith(t, genius, &search.TrackerSongs{Err: errors.New("sheets down")}, nil); err == nil {
		t.Error("List succeeded with both sources failing")
	}
}

func TestService_List_CancelledJobIsAnErrorEvenWithTrackerSongs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Dream [V2]", "dreamv2a")}}
	svc := searchtest.MustUnreleased(search.NewUnreleasedService(search.NoUnreleasedGenius{}, &search.ArtistCatalogTitles{}, cachedConfig, searchtest.UnreleasedWiring(search.NoUnreleasedCache{}, searchtest.UnreleasedTrackers(trackers))))
	if _, err := svc.List(ctx, search.ProviderDeezer, "1", "Ken Carson", nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

func TestService_List_APartTrackerReadKeepsItsSongsButCachesShort(t *testing.T) {
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Loot", "lootaaaa")}, Err: search.ErrTrackerPartial}
	cache := newMemoryCache()
	got, err := listWith(t, nil, trackers, cache)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v; want the one song read", titlesOf(got), err)
	}
	for key, ttl := range cache.ttls {
		if ttl != shortTTL {
			t.Errorf("ttl[%s] = %v, want the short TTL for a part read", key, ttl)
		}
	}
}

func TestService_List_AnOffStreamingTrackerSongKeepsTheGeniusQuality(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Loot")}
	genius.Songs[0].Quality = "CDQ"
	tracked := search.UnreleasedSong{Title: "Loot", TrackerKey: "ken:loot", OffStreaming: true}
	got, err := listWith(t, genius, &search.TrackerSongs{Songs: []search.UnreleasedSong{tracked}}, nil)
	if err != nil || len(got) != 1 || got[0].Quality != "CDQ" || !got[0].OffStreaming {
		t.Errorf("got %+v, err %v; want CDQ kept and the off-streaming flag", got, err)
	}
}

func TestService_List_ALinkOutTrackerRowLeavesHasMediaFalseAndCarriesOpenURL(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Loot")}
	hasMedia := false
	linkOut := search.UnreleasedSong{Title: "Loot", TrackerKey: "ken:loot", HasMedia: &hasMedia, OpenURL: "https://mega.nz/file/abc", OpenHost: "mega.nz"}
	got, err := listWith(t, genius, &search.TrackerSongs{Songs: []search.UnreleasedSong{linkOut}}, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v, want one merged item", titlesOf(got), err)
	}
	if got[0].HasMedia != nil && *got[0].HasMedia || got[0].OpenURL != "https://mega.nz/file/abc" || got[0].OpenHost != "mega.nz" {
		t.Errorf("merged = %+v, want has_media not true and the mega open link", got[0])
	}
}

func TestService_List_GeniusSongJoinsTheTrackerSongWithTheSameBaseTitle(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("tragic")}
	genius.Songs[0].GeniusURL = "https://genius.com/tragic"
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("tragic [V1]", "tragicaa")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v, want one joined song", titlesOf(got), err)
	}
	song := got[0]
	if song.Title != "tragic" || song.GeniusURL != "https://genius.com/tragic" || song.GeniusID != 1 {
		t.Errorf("song = %+v, want the genius identity", song)
	}
	if !slices.Contains(song.AudioLinks, pixeldrainLink("tragicaa")) || song.HasMedia == nil || !*song.HasMedia {
		t.Errorf("song = %+v, want the pixeldrain link and has_media true", song)
	}
}

func TestService_List_GeniusLabelledSongStaysApartFromTheTrackerSong(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("tragic (Live from SNL)", "tragic (Remix)")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("tragic", "tragicaa")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"tragic (Live from SNL)", "tragic (Remix)", "tragic"}
	if len(got) != 3 || !slices.Equal(titlesOf(got), want) {
		t.Fatalf("titles = %v, want %v", titlesOf(got), want)
	}
	if got[0].Quality != "" || got[2].GeniusID != 0 {
		t.Errorf("got %+v, want the labelled songs untouched", got)
	}
}

func TestService_List_TwoGeniusSongsWithOneTitleBothTakeTheTrackerFile(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Loot", "Loot")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Loot", "lootaaaa")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil || len(got) != 2 {
		t.Fatalf("got %v, err %v, want two songs", titlesOf(got), err)
	}
	for _, song := range got {
		if !slices.Contains(song.AudioLinks, pixeldrainLink("lootaaaa")) {
			t.Errorf("song = %+v, want the pixeldrain link", song)
		}
	}
}

func TestService_List_GeniusSongWithTheExactTrackerTitleKeepsTheTrackerVersions(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("Me")}
	tracked := trackerSong("Me", "meaaaaaa")
	tracked.Versions = []search.UnreleasedVersion{{Title: "Me [V1]", Label: "V1"}, {Title: "Me [V2]", Label: "V2"}}
	got, err := listWith(t, genius, &search.TrackerSongs{Songs: []search.UnreleasedSong{tracked}}, nil)
	if err != nil || len(got) != 1 {
		t.Fatalf("got %v, err %v, want one joined song", titlesOf(got), err)
	}
	if song := got[0]; song.GeniusID != 1 || len(song.Versions) != 2 {
		t.Errorf("song = %+v, want the genius identity and both tracker versions", song)
	}
}

func TestService_List_DemoGeniusSongJoinsItsBaseTrackSongAsAVersion(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("greedy (Demo 1)", "Greedy (Live)", "Borrow me (Voice memo)")}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Greedy", "greedyaa"), trackerSong("Borrow me", "borrowaa")}}
	got, err := listWith(t, genius, trackers, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"Greedy (Live)", "Greedy", "Borrow me"}
	if len(got) != len(want) || !slices.Equal(titlesOf(got), want) {
		t.Fatalf("titles = %v, want %v", titlesOf(got), want)
	}
	greedy := got[1]
	if len(greedy.Versions) != 2 || greedy.Versions[1].Title != "greedy (Demo 1)" || greedy.Versions[1].Label != "Demo 1" {
		t.Errorf("greedy = %+v, want the demo as a second version", greedy)
	}
	if greedy.HasMedia == nil || !*greedy.HasMedia || !slices.Contains(greedy.AudioLinks, pixeldrainLink("greedyaa")) {
		t.Errorf("greedy = %+v, want the base audio kept", greedy)
	}
	if len(got[2].Versions) != 2 || got[2].Versions[1].Label != "Voice Memo" {
		t.Errorf("borrow me = %+v, want the voice memo as a version", got[2])
	}
}

func TestService_List_DemoGeniusSongJoinsTheGeniusBaseSong(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("bedroom (demo)", "bedroom")}
	got, err := listWith(t, genius, &search.TrackerSongs{}, nil)
	if err != nil || len(got) != 1 || got[0].Title != "bedroom" || len(got[0].Versions) != 2 {
		t.Fatalf("got %+v, err %v, want one bedroom card with two versions", got, err)
	}
}

func TestService_List_DemoWithoutABaseSongStaysItsOwnCard(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs("lonely (Demo)")}
	got, err := listWith(t, genius, &search.TrackerSongs{}, nil)
	if err != nil || len(got) != 1 || got[0].Title != "lonely (Demo)" {
		t.Fatalf("got %v, err %v, want the demo kept", titlesOf(got), err)
	}
}

func mergedTitles(t *testing.T, geniusTitles []string, trackerTitles ...string) []search.UnreleasedSong {
	t.Helper()
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs(geniusTitles...)}
	tracked := make([]search.UnreleasedSong, 0, len(trackerTitles))
	for i, title := range trackerTitles {
		tracked = append(tracked, trackerSong(title, "id"+string(rune('a'+i))))
	}
	got, err := listWith(t, genius, &search.TrackerSongs{Songs: tracked}, nil)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return got
}

func TestService_List_CensoredGeniusTitleJoinsTheUncensoredTrackerSong(t *testing.T) {
	got := mergedTitles(t, []string{"Crashed Dat B*tch"}, "Crashed dat bitch [V1]")
	if len(got) != 1 || len(got[0].AudioLinks) != 1 || got[0].Title != "Crashed Dat B*tch" {
		t.Fatalf("got %+v, want one card with the tracker audio", got)
	}
}

func TestService_List_NearMatchJoinsMisspeltAndTransliteratedTitles(t *testing.T) {
	for _, pair := range [][2]string{{"Flawl3ss", "Flawless"}, {"Straße", "Strasse"}, {"Новый", "Novyi"}} {
		got := mergedTitles(t, []string{pair[0]}, pair[1]+" [V1]")
		if len(got) != 1 || len(got[0].AudioLinks) != 1 {
			t.Errorf("%q + %q = %v, want one joined card", pair[0], pair[1], titlesOf(got))
		}
	}
}

func TestService_List_NearMatchKeepsSignAndSignsApart(t *testing.T) {
	got := mergedTitles(t, []string{"Sign"}, "Signs [V1]")
	if len(got) != 2 {
		t.Fatalf("got %v, want two cards", titlesOf(got))
	}
}

func TestService_List_CensoredStarsStandForOneOrMoreLetters(t *testing.T) {
	got := mergedTitles(t, []string{"B**ch Better"}, "Bitch Better [V1]")
	if len(got) != 1 || len(got[0].AudioLinks) != 1 || got[0].Title != "B**ch Better" {
		t.Fatalf("got %+v, want one card with the tracker audio", got)
	}
}

func TestService_List_NearMatchKeepsDifferentSongsApart(t *testing.T) {
	pairs := [][2]string{{"Mask Off", "Mask On"}, {"Bad Guy", "Bad Boy"}, {"Hotel Room", "Hotel Rooms"}, {"Lucid Dreams", "Lucid Dream"}, {"Sicko Mode", "Sicko Mood"}}
	for _, pair := range pairs {
		got := mergedTitles(t, []string{pair[0]}, pair[1]+" [V1]")
		if len(got) != 2 {
			t.Errorf("%q + %q = %v, want two cards", pair[0], pair[1], titlesOf(got))
		}
	}
}

func TestService_List_NearMatchSkipsAnAmbiguousCandidate(t *testing.T) {
	got := mergedTitles(t, []string{"Flawless1", "Flawless2"}, "Flawless [V1]")
	if len(got) != 3 {
		t.Fatalf("got %v, want the tracker song kept apart", titlesOf(got))
	}
}

func TestService_List_NonTrackPagesAreDroppedAndCounted(t *testing.T) {
	logs := captureLogs(t)
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: songs(
		"Miss Possessive Tour Setlist (2024)",
		"End Credits",
		"Tour Dates",
		"Setlist",
	)}
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{trackerSong("Setlist [V1]", "s1")}}

	got, err := listWith(t, genius, trackers, nil)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	want := []string{"End Credits", "Setlist"}
	if !slices.Equal(titlesOf(got), want) {
		t.Errorf("titles = %v, want %v", titlesOf(got), want)
	}
	if !strings.Contains(logs.String(), "non_song_dropped=2") {
		t.Errorf("coverage log should count the 2 dropped pages: %s", logs.String())
	}
	if len(got) != 2 || got[1].TrackerKey == "" || len(got[1].AudioLinks) != 1 {
		t.Errorf("Setlist joined to its tracker file should keep the pixeldrain link, got %+v", got)
	}
}
