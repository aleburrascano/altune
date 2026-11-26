package handler_test

import (
	"altune/go-api/internal/search"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func unreleasedRouter(genius *search.FakeGeniusSongCatalog, catalog []string) chi.Router {
	svc := search.NewUnreleasedService(genius, &search.ArtistCatalogTitles{Titles: catalog}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10})
	h := search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func decodeUnreleased(t *testing.T, body []byte) search.UnreleasedResponseDTO {
	t.Helper()
	var dto search.UnreleasedResponseDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return dto
}

func TestHandleArtistUnreleased_NoGeniusMatchIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&search.FakeGeniusSongCatalog{ArtistFound: false}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/999/unreleased?name=Nobody", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":false}\n" {
		t.Errorf("body = %q, want an empty items list", got)
	}
}

func TestHandleArtistUnreleased_WithoutAnArtistNameIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&search.FakeGeniusSongCatalog{ArtistFound: true}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ServiceNotConfiguredIsAnEmptyList(t *testing.T) {
	h := search.NewDiscoveryHandler(search.DiscoveryServices{})
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ReturnsUnreleasedSongsWithTheAppShape(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []search.UnreleasedSong{
			{GeniusID: 1, Title: "Magnolia", GeniusURL: "https://genius.com/m"},
			{GeniusID: 2, Title: "Cancun", GeniusURL: "https://genius.com/c", CoverURL: "https://img/c.jpg", ReleaseDate: "2022-07-24"},
			{GeniusID: 3, Title: "Naruto", GeniusURL: "https://genius.com/n"},
		},
		MediaByID: map[int]bool{2: true},
	}
	router := unreleasedRouter(genius, []string{"Magnolia"})

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Playboi+Carti", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 || items[0].Title != "Cancun" || items[1].Title != "Naruto" {
		t.Fatalf("items = %+v, want Cancun then Naruto", items)
	}
	cancun, naruto := items[0], items[1]
	if cancun.CoverURL == nil || *cancun.CoverURL != "https://img/c.jpg" || cancun.ReleaseDate == nil || cancun.HasMedia == nil || !*cancun.HasMedia {
		t.Errorf("cancun = %+v, want cover, release date and has_media", cancun)
	}
	if naruto.CoverURL != nil || naruto.ReleaseDate != nil || (naruto.HasMedia != nil && *naruto.HasMedia) {
		t.Errorf("naruto = %+v, want null cover, null release date and no media", naruto)
	}
}

func TestHandleArtistUnreleased_UpstreamFailureIsAnError(t *testing.T) {
	router := unreleasedRouter(&search.FakeGeniusSongCatalog{ArtistFound: true, Err: errors.New("genius unavailable")}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	if rec.Code < 500 {
		t.Errorf("status = %d, want a 5xx", rec.Code)
	}
}

func TestHandleArtistUnreleased_SlowJobAnswersPendingWithNoItems(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: []search.UnreleasedSong{{GeniusID: 1, Title: "Leak"}}, Gate: make(chan struct{})}
	svc := search.NewUnreleasedService(genius, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10, Wait: 10 * time.Millisecond})
	h := search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":true}\n" {
		t.Errorf("body = %q, want pending with no items", got)
	}
}

func TestHandleArtistUnreleased_ReturnsOpenURLAndOpenHost(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []search.UnreleasedSong{{GeniusID: 1, Title: "Cancun", OpenURL: "https://mega.nz/file/abc", OpenHost: "mega.nz"}},
	}
	rec := discServe(t, unreleasedRouter(genius, nil), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); !strings.Contains(body, `"open_url":"https://mega.nz/file/abc"`) || !strings.Contains(body, `"open_host":"mega.nz"`) {
		t.Errorf("body = %s, want open_url and open_host", body)
	}
}

func TestHandleArtistUnreleased_ReturnsAudioLinksAsAlwaysAnArray(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []search.UnreleasedSong{
			{GeniusID: 1, Title: "Cancun", GeniusURL: "https://genius.com/c"},
			{GeniusID: 2, Title: "Naruto", GeniusURL: "https://genius.com/n"},
		},
		DetailByID: map[int]search.GeniusSongDetail{
			1: {HasMedia: true, AudioLinks: []search.AudioLink{{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}}},
		},
	}
	router := unreleasedRouter(genius, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	want := search.AudioLink{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}
	if len(items) != 2 || len(items[0].AudioLinks) != 1 || items[0].AudioLinks[0] != want {
		t.Fatalf("items = %+v, want the soundcloud link on Cancun", items)
	}
	if !strings.Contains(rec.Body.String(), `"audio_links":[]`) {
		t.Errorf("body = %s, want an empty audio_links array for Naruto", rec.Body.String())
	}
}

func ownedUnreleasedRouter(t *testing.T, reader goldenOwnershipReader) chi.Router {
	genius := &search.FakeGeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []search.UnreleasedSong{
			{GeniusID: 1, Title: "Dream", GeniusURL: "https://genius.com/d"},
			{GeniusID: 2, Title: "Loot", GeniusURL: "https://genius.com/l"},
		},
	}
	svc := search.NewUnreleasedService(genius, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10})
	h := wireGoldenOwnership(t, search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc), reader, nil)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleArtistUnreleased_StampsTheSongsTheUserOwns(t *testing.T) {
	reader := goldenOwnershipReader{owned: map[string]search.OwnedTrack{
		search.OwnershipKey("Dream", "Ken Carson"): {TrackID: "track-9", AcquisitionStatus: "ready"},
	}}

	rec := discServe(t, ownedUnreleasedRouter(t, reader), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 || items[0].Extras[search.ExtraOwnedTrackID] != "track-9" || items[0].Extras["owned_acquisition_status"] != "ready" {
		t.Fatalf("items = %+v, want Dream stamped as owned track-9, ready", items)
	}
	if _, owned := items[1].Extras[search.ExtraOwnedTrackID]; owned {
		t.Errorf("Loot extras = %v, want no ownership", items[1].Extras)
	}
}

func TestHandleArtistUnreleased_LeavesSongsUnstampedForAUserWhoOwnsNone(t *testing.T) {
	rec := discServe(t, ownedUnreleasedRouter(t, goldenOwnershipReader{}), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	for _, item := range decodeUnreleased(t, rec.Body.Bytes()).Items {
		if len(item.Extras) != 0 {
			t.Errorf("%s extras = %v, want none", item.Title, item.Extras)
		}
	}
}

func TestHandleArtistUnreleased_TrackerOnlyItemHasNullGeniusFieldsOnTheWire(t *testing.T) {
	trackers := &search.TrackerSongs{Songs: []search.UnreleasedSong{{
		Title: "Dream [V2]", Quality: "High Quality", TrackerKey: "ken-carson:dream-v2", HasMedia: new(true), OffStreaming: true,
		AudioLinks: []search.AudioLink{{Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://pixeldrain.com/u/ozSpSjn7"}},
	}}}
	svc := search.NewUnreleasedService(nil, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10}, search.WithUnreleasedTrackers(trackers))
	h := search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	want := `{"items":[{"genius_id":null,"title":"Dream [V2]","cover_url":null,"genius_url":null,"release_date":null,"has_media":true,"quality":"High Quality","quality_rank":3,"tracker_key":"ken-carson:dream-v2","off_streaming":true,"audio_links":[{"provider":"pixeldrain","external_id":"ozSpSjn7","url":"https://pixeldrain.com/u/ozSpSjn7"}],"extras":{},"best_version_title":"Dream [V2]"}],"pending":false}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestHandleArtistUnreleased_QualityRankIsTheServersRankAndZeroWhenUnknown(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: []search.UnreleasedSong{
		{GeniusID: 1, Title: "Ranked", Quality: "Lossless"},
		{GeniusID: 2, Title: "Unranked"},
	}}
	rec := discServe(t, unreleasedRouter(genius, nil), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var wire struct {
		Items []struct {
			QualityRank int `json:"quality_rank"`
		} `json:"items"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if len(wire.Items) != 2 || wire.Items[0].QualityRank != 5 || wire.Items[1].QualityRank != 0 {
		t.Errorf("items = %+v, want ranks 5 then 0", wire.Items)
	}
}

func TestHandleArtistUnreleased_GeniusItemKeepsItsIDAndURLAndNullTrackerFields(t *testing.T) {
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: []search.UnreleasedSong{{GeniusID: 9, Title: "Leak", GeniusURL: "https://genius.com/l"}}}
	rec := discServe(t, unreleasedRouter(genius, nil), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	item := decodeUnreleased(t, rec.Body.Bytes()).Items[0]
	if item.GeniusID == nil || *item.GeniusID != 9 || item.GeniusURL == nil || item.Quality != nil || item.TrackerKey != nil {
		t.Errorf("item = %+v", item)
	}
}

func TestUnreleasedSongDTO_CarriesEveryDomainField(t *testing.T) {
	wire := map[string]bool{}
	dto := reflect.TypeFor[search.UnreleasedSongDTO]()
	for i := range dto.NumField() {
		name, _, _ := strings.Cut(dto.Field(i).Tag.Get("json"), ",")
		wire[name] = true
	}
	song := reflect.TypeFor[search.UnreleasedSong]()
	for i := range song.NumField() {
		if name, _, _ := strings.Cut(song.Field(i).Tag.Get("json"), ","); !wire[name] {
			t.Errorf("domain field %s (json %q) never reaches the response", song.Field(i).Name, name)
		}
	}
}

func TestHandleArtistUnreleased_StoreBuiltArtistAnswersFromTheStoreWithNoGeniusCalls(t *testing.T) {
	key := search.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: "27"}
	store := &search.FakeUnreleasedStore{Artists: map[search.UnreleasedArtistKey]search.StoredArtist{key: {
		Complete: true, Built: true, BuildVersion: search.UnreleasedCurrentBuildVersion,
		Songs: []search.UnreleasedSong{{GeniusID: 1, Title: "A"}, {GeniusID: 2, Title: "B"}, {GeniusID: 3, Title: "C"}},
	}}}
	genius := &search.FakeGeniusSongCatalog{ArtistFound: true, Songs: []search.UnreleasedSong{{GeniusID: 9, Title: "Other"}}}
	svc := search.NewUnreleasedService(genius, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10}, search.WithUnreleasedStore(store))
	h := search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/27/unreleased?name=Drake", nil)

	discAssertStatus(t, rec, http.StatusOK)
	dto := decodeUnreleased(t, rec.Body.Bytes())
	if len(dto.Items) != 3 || dto.Pending {
		t.Errorf("response = %+v, want 3 items and pending false", dto)
	}
	if len(genius.SearchedFor) != 0 {
		t.Errorf("genius searched %v, want 0 calls", genius.SearchedFor)
	}
}

func TestHandleArtistUnreleased_OwnedVersionIsStampedByItsFullTitleNotTheBaseTitle(t *testing.T) {
	link := []search.AudioLink{{Provider: search.AudioProviderYouTube, ExternalID: "v3", URL: "https://youtu.be/v3"}}
	genius := &search.FakeGeniusSongCatalog{
		ArtistFound: true,
		Songs: []search.UnreleasedSong{
			{GeniusID: 1, Title: "Sports Car", Versions: []search.UnreleasedVersion{
				{Title: "Sports Car [V1]", Label: "V1"},
				{Title: "Sports Car [V3]", Label: "V3", AudioLinks: link},
			}},
			{GeniusID: 2, Title: "Other", Versions: []search.UnreleasedVersion{{Title: "Other [V1]"}}},
		},
		DetailByID: map[int]search.GeniusSongDetail{1: {AudioLinks: link}},
	}
	svc := search.NewUnreleasedService(genius, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10})
	owned := goldenOwnershipReader{owned: map[string]search.OwnedTrack{
		search.OwnershipKey("Sports Car [V3]", "Carti"): {TrackID: "saved-v3", AcquisitionStatus: "ready"},
		search.OwnershipKey("Other", "Carti"):           {TrackID: "streaming-other", AcquisitionStatus: "ready"},
	}}
	h := wireGoldenOwnership(t, search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc), owned, nil)
	router := chi.NewRouter()
	router.Use(discAsTestUser)
	router.Mount("/discovery", h.Routes())

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Carti", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	sportsCar, other := items[0], items[1]
	if got := sportsCar.Versions[1].Extras["owned_track_id"]; got != "saved-v3" {
		t.Errorf("V3 owned_track_id = %v, want saved-v3", got)
	}
	if _, has := sportsCar.Versions[0].Extras["owned_track_id"]; has {
		t.Errorf("V1 extras = %v, want not owned", sportsCar.Versions[0].Extras)
	}
	if got := sportsCar.Extras["owned_track_id"]; got != "saved-v3" {
		t.Errorf("song owned_track_id = %v, want its best version's saved-v3", got)
	}
	if _, has := other.Extras["owned_track_id"]; has {
		t.Errorf("other song extras = %v, base title must not mark a grouped song owned", other.Extras)
	}
}

func TestHandleArtistUnreleased_BestVersionTitleIsTheServersPickedVersion(t *testing.T) {
	link := []search.AudioLink{{Provider: search.AudioProviderYouTube, ExternalID: "v3", URL: "https://youtu.be/v3"}}
	versions := []search.UnreleasedVersion{
		{Title: "Sports Car [V1]", Label: "V1", AudioLinks: link},
		{Title: "Sports Car [V3]", Label: "V3", AudioLinks: link},
	}
	genius := &search.FakeGeniusSongCatalog{
		ArtistFound: true,
		Songs: []search.UnreleasedSong{
			{GeniusID: 1, Title: "Sports Car", Versions: versions},
			{GeniusID: 2, Title: "Other"},
		},
		DetailByID: map[int]search.GeniusSongDetail{1: {AudioLinks: link}},
	}
	router := unreleasedRouter(genius, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Carti", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 {
		t.Fatalf("items = %+v, want 2", items)
	}
	best := search.UnreleasedSong{Versions: versions}.BestVersion().Title
	if items[0].BestVersionTitle != best {
		t.Errorf("grouped best_version_title = %q, want %q", items[0].BestVersionTitle, best)
	}
	if items[1].BestVersionTitle != "Other" {
		t.Errorf("single best_version_title = %q, want its own title", items[1].BestVersionTitle)
	}
}

func unreleasedSearchRouter(store *search.FakeUnreleasedStore) chi.Router {
	svc := search.NewUnreleasedService(&search.FakeGeniusSongCatalog{}, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{MaxPages: 1, MaxDetails: 10}, search.WithUnreleasedStore(store))
	h := search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(discAsTestUser)
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleUnreleasedSearch_ReturnsHitsInStoreOrderWithTheirArtist(t *testing.T) {
	carti := search.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: "42"}
	store := &search.FakeUnreleasedStore{Hits: []search.UnreleasedHit{
		{Artist: carti, ArtistName: "Playboi Carti", Song: search.UnreleasedSong{GeniusID: 1, Title: "Cancún?", Quality: "lossless"}},
		{Artist: search.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: "7"}, ArtistName: "Don Toliver", Song: search.UnreleasedSong{GeniusID: 2, Title: "Cancun Remix"}},
	}}

	rec := discServe(t, unreleasedSearchRouter(store), http.MethodGet, "/discovery/unreleased/search?q=cancun", nil)

	discAssertStatus(t, rec, http.StatusOK)
	var body search.UnreleasedSearchResponseDTO
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.Bytes(), err)
	}
	if len(body.Items) != 2 || body.Items[0].Title != "Cancún?" || body.Items[1].Title != "Cancun Remix" {
		t.Fatalf("items = %+v, want the store order", body.Items)
	}
	if got, want := body.Items[0].Artist, (search.UnreleasedArtistDTO{Provider: "deezer", ExternalID: "42", Name: "Playboi Carti"}); got != want {
		t.Errorf("artist = %+v, want %+v", got, want)
	}
	if body.Items[0].QualityRank != 5 || body.Items[0].AudioLinks == nil {
		t.Errorf("item = %+v, want the artist endpoint's item shape", body.Items[0].UnreleasedSongDTO)
	}
	if got := store.Queries(); !reflect.DeepEqual(got, []string{"cancun"}) {
		t.Errorf("store queries = %v, want [cancun]", got)
	}
}

func TestHandleUnreleasedSearch_AOneCharacterQueryIsAnEmptyListAndSkipsTheStore(t *testing.T) {
	store := &search.FakeUnreleasedStore{Hits: []search.UnreleasedHit{{ArtistName: "A", Song: search.UnreleasedSong{Title: "x"}}}}

	rec := discServe(t, unreleasedSearchRouter(store), http.MethodGet, "/discovery/unreleased/search?q=x", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[]}\n" {
		t.Errorf("body = %q, want an empty items list", got)
	}
	if got := store.Queries(); len(got) != 0 {
		t.Errorf("store queries = %v, want none", got)
	}
}

func TestHandleUnreleasedSearch_StoreFailureIsAnError(t *testing.T) {
	store := &search.FakeUnreleasedStore{SearchErr: errors.New("db down")}

	rec := discServe(t, unreleasedSearchRouter(store), http.MethodGet, "/discovery/unreleased/search?q=cancun", nil)

	if rec.Code < 500 {
		t.Errorf("status = %d, want a 5xx", rec.Code)
	}
}

func TestArtistUnreleased_KnownIDsKeepValidEntriesAndSkipTheRest(t *testing.T) {
	const maxKnownIDs = 16
	tooMany := strings.Repeat("deezer:1,", maxKnownIDs) + "spotify:late"
	cases := []struct {
		ids  string
		want map[search.ProviderName]string
	}{
		{ids: "", want: nil},
		{ids: "spotify:sp1, applemusic:123", want: map[search.ProviderName]string{search.ProviderSpotify: "sp1", search.ProviderAppleMusic: "123"}},
		{ids: "spotify:..,spotify,notaprovider:x,applemusic:a/b,deezer:9", want: map[search.ProviderName]string{search.ProviderDeezer: "9"}},
		{ids: tooMany, want: map[search.ProviderName]string{search.ProviderDeezer: "1"}},
	}
	key := search.UnreleasedArtistKey{Provider: search.ProviderDeezer, ExternalID: "27"}
	for _, tc := range cases {
		store := &recordingUnreleasedStore{FakeUnreleasedStore: &search.FakeUnreleasedStore{
			Artists: map[search.UnreleasedArtistKey]search.StoredArtist{
				key: {Built: true, BuildVersion: search.UnreleasedCurrentBuildVersion},
			},
		}}
		svc := search.NewUnreleasedService(nil, &search.ArtistCatalogTitles{}, nil, search.UnreleasedConfig{}, search.WithUnreleasedStore(store))
		r := chi.NewRouter()
		r.Use(discAsTestUser)
		r.Mount("/discovery", search.NewDiscoveryHandler(search.DiscoveryServices{}).WithUnreleased(svc).Routes())

		rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/27/unreleased?name=Nas&ids="+url.QueryEscape(tc.ids), nil)

		discAssertStatus(t, rec, http.StatusOK)
		if len(store.artists) != 1 || !maps.Equal(store.artists[0].known, tc.want) {
			t.Errorf("ids %q: recorded %+v, want known %v", tc.ids, store.artists, tc.want)
		}
	}
}
