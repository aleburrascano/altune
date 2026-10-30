package handler

import (
	"altune/go-api/internal/auth/adapters/middleware"
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service/unreleased"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

func unreleasedRouter(genius *discoverytest.GeniusSongCatalog, catalog []string) chi.Router {
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{Titles: catalog}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10})
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func decodeUnreleased(t *testing.T, body []byte) UnreleasedResponseDTO {
	t.Helper()
	var dto UnreleasedResponseDTO
	if err := json.Unmarshal(body, &dto); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	return dto
}

func TestHandleArtistUnreleased_NoGeniusMatchIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: false}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/999/unreleased?name=Nobody", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":false}\n" {
		t.Errorf("body = %q, want an empty items list", got)
	}
}

func TestHandleArtistUnreleased_WithoutAnArtistNameIsAnEmptyList(t *testing.T) {
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: true}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ServiceNotConfiguredIsAnEmptyList(t *testing.T) {
	h := NewDiscoveryHandler(DiscoveryServices{})
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if items := decodeUnreleased(t, rec.Body.Bytes()).Items; len(items) != 0 {
		t.Errorf("items = %v, want none", items)
	}
}

func TestHandleArtistUnreleased_ReturnsUnreleasedSongsWithTheAppShape(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
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
	router := unreleasedRouter(&discoverytest.GeniusSongCatalog{ArtistFound: true, Err: errors.New("genius unavailable")}, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Don", nil)

	if rec.Code < 500 {
		t.Errorf("status = %d, want a 5xx", rec.Code)
	}
}

func TestHandleArtistUnreleased_SlowJobAnswersPendingWithNoItems(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: []domain.UnreleasedSong{{GeniusID: 1, Title: "Leak"}}, Gate: make(chan struct{})}
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10, Wait: 10 * time.Millisecond})
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if got := rec.Body.String(); got != "{\"items\":[],\"pending\":true}\n" {
		t.Errorf("body = %q, want pending with no items", got)
	}
}

func TestHandleArtistUnreleased_ReturnsOpenURLAndOpenHost(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{{GeniusID: 1, Title: "Cancun", OpenURL: "https://mega.nz/file/abc", OpenHost: "mega.nz"}},
	}
	rec := discServe(t, unreleasedRouter(genius, nil), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	if body := rec.Body.String(); !strings.Contains(body, `"open_url":"https://mega.nz/file/abc"`) || !strings.Contains(body, `"open_host":"mega.nz"`) {
		t.Errorf("body = %s, want open_url and open_host", body)
	}
}

func TestHandleArtistUnreleased_ReturnsAudioLinksAsAlwaysAnArray(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
			{GeniusID: 1, Title: "Cancun", GeniusURL: "https://genius.com/c"},
			{GeniusID: 2, Title: "Naruto", GeniusURL: "https://genius.com/n"},
		},
		DetailByID: map[int]domain.GeniusSongDetail{
			1: {HasMedia: true, AudioLinks: []domain.AudioLink{{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}}},
		},
	}
	router := unreleasedRouter(genius, nil)

	rec := discServe(t, router, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	want := domain.AudioLink{Provider: "soundcloud", URL: "https://soundcloud.com/a/b"}
	if len(items) != 2 || len(items[0].AudioLinks) != 1 || items[0].AudioLinks[0] != want {
		t.Fatalf("items = %+v, want the soundcloud link on Cancun", items)
	}
	if !strings.Contains(rec.Body.String(), `"audio_links":[]`) {
		t.Errorf("body = %s, want an empty audio_links array for Naruto", rec.Body.String())
	}
}

func ownedUnreleasedRouter(reader goldenOwnershipReader) chi.Router {
	genius := &discoverytest.GeniusSongCatalog{
		ArtistID: 7, ArtistFound: true,
		Songs: []domain.UnreleasedSong{
			{GeniusID: 1, Title: "Dream", GeniusURL: "https://genius.com/d"},
			{GeniusID: 2, Title: "Loot", GeniusURL: "https://genius.com/l"},
		},
	}
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10})
	h := wireGoldenOwnership(NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc), reader, nil)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())
	return r
}

func TestHandleArtistUnreleased_StampsTheSongsTheUserOwns(t *testing.T) {
	reader := goldenOwnershipReader{owned: map[string]ports.OwnedTrack{
		domain.OwnershipKey("Dream", "Ken Carson"): {TrackID: "track-9", AcquisitionStatus: "ready"},
	}}

	rec := discServe(t, ownedUnreleasedRouter(reader), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	items := decodeUnreleased(t, rec.Body.Bytes()).Items
	if len(items) != 2 || items[0].Extras[domain.ExtraOwnedTrackID] != "track-9" || items[0].Extras["owned_acquisition_status"] != "ready" {
		t.Fatalf("items = %+v, want Dream stamped as owned track-9, ready", items)
	}
	if _, owned := items[1].Extras[domain.ExtraOwnedTrackID]; owned {
		t.Errorf("Loot extras = %v, want no ownership", items[1].Extras)
	}
}

func TestHandleArtistUnreleased_LeavesSongsUnstampedForAUserWhoOwnsNone(t *testing.T) {
	rec := discServe(t, ownedUnreleasedRouter(goldenOwnershipReader{}), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	for _, item := range decodeUnreleased(t, rec.Body.Bytes()).Items {
		if len(item.Extras) != 0 {
			t.Errorf("%s extras = %v, want none", item.Title, item.Extras)
		}
	}
}

func TestHandleArtistUnreleased_TrackerOnlyItemHasNullGeniusFieldsOnTheWire(t *testing.T) {
	trackers := &discoverytest.TrackerSongs{Songs: []domain.UnreleasedSong{{
		Title: "Dream [V2]", Quality: "High Quality", TrackerKey: "ken-carson:dream-v2", HasMedia: new(true), OffStreaming: true,
		AudioLinks: []domain.AudioLink{{Provider: "pixeldrain", ExternalID: "ozSpSjn7", URL: "https://pixeldrain.com/u/ozSpSjn7"}},
	}}}
	svc := unreleased.NewService(nil, &discoverytest.ArtistCatalogTitles{}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10}, unreleased.WithTrackers(trackers))
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
	r.Mount("/discovery", h.Routes())

	rec := discServe(t, r, http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Ken+Carson", nil)

	discAssertStatus(t, rec, http.StatusOK)
	want := `{"items":[{"genius_id":null,"title":"Dream [V2]","cover_url":null,"genius_url":null,"release_date":null,"has_media":true,"quality":"High Quality","tracker_key":"ken-carson:dream-v2","off_streaming":true,"audio_links":[{"provider":"pixeldrain","external_id":"ozSpSjn7","url":"https://pixeldrain.com/u/ozSpSjn7"}],"extras":{}}],"pending":false}` + "\n"
	if got := rec.Body.String(); got != want {
		t.Errorf("body = %s, want %s", got, want)
	}
}

func TestHandleArtistUnreleased_GeniusItemKeepsItsIDAndURLAndNullTrackerFields(t *testing.T) {
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: []domain.UnreleasedSong{{GeniusID: 9, Title: "Leak", GeniusURL: "https://genius.com/l"}}}
	rec := discServe(t, unreleasedRouter(genius, nil), http.MethodGet, "/discovery/artists/deezer/1/unreleased?name=Artist", nil)

	discAssertStatus(t, rec, http.StatusOK)
	item := decodeUnreleased(t, rec.Body.Bytes()).Items[0]
	if item.GeniusID == nil || *item.GeniusID != 9 || item.GeniusURL == nil || item.Quality != nil || item.TrackerKey != nil {
		t.Errorf("item = %+v", item)
	}
}

func TestUnreleasedSongDTO_CarriesEveryDomainField(t *testing.T) {
	wire := map[string]bool{}
	dto := reflect.TypeFor[UnreleasedSongDTO]()
	for i := range dto.NumField() {
		name, _, _ := strings.Cut(dto.Field(i).Tag.Get("json"), ",")
		wire[name] = true
	}
	song := reflect.TypeFor[domain.UnreleasedSong]()
	for i := range song.NumField() {
		if name, _, _ := strings.Cut(song.Field(i).Tag.Get("json"), ","); !wire[name] {
			t.Errorf("domain field %s (json %q) never reaches the response", song.Field(i).Name, name)
		}
	}
}

func TestHandleArtistUnreleased_StoreBuiltArtistAnswersFromTheStoreWithNoGeniusCalls(t *testing.T) {
	key := ports.UnreleasedArtistKey{Provider: domain.ProviderDeezer, ExternalID: "27"}
	store := &discoverytest.UnreleasedStore{Artists: map[ports.UnreleasedArtistKey]discoverytest.StoredArtist{key: {
		Complete: true, Built: true,
		Songs: []domain.UnreleasedSong{{GeniusID: 1, Title: "A"}, {GeniusID: 2, Title: "B"}, {GeniusID: 3, Title: "C"}},
	}}}
	genius := &discoverytest.GeniusSongCatalog{ArtistFound: true, Songs: []domain.UnreleasedSong{{GeniusID: 9, Title: "Other"}}}
	svc := unreleased.NewService(genius, &discoverytest.ArtistCatalogTitles{}, nil, unreleased.Config{MaxPages: 1, MaxDetails: 10}, unreleased.WithStore(store))
	h := NewDiscoveryHandler(DiscoveryServices{}).WithUnreleased(svc)
	r := chi.NewRouter()
	r.Use(middleware.Middleware(discVerifyAsTestUser))
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
