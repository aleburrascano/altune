package setup

import (
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/providers/applemusic"
	"altune/go-api/internal/search/internal/adapters/providers/coverartarchive"
	"altune/go-api/internal/search/internal/adapters/providers/deezer"
	"altune/go-api/internal/search/internal/adapters/providers/discogs"
	"altune/go-api/internal/search/internal/adapters/providers/fanarttv"
	"altune/go-api/internal/search/internal/adapters/providers/fansheets"
	"altune/go-api/internal/search/internal/adapters/providers/genius"
	"altune/go-api/internal/search/internal/adapters/providers/itunes"
	"altune/go-api/internal/search/internal/adapters/providers/musicbrainz"
	"altune/go-api/internal/search/internal/adapters/providers/soundcloud"
	"altune/go-api/internal/search/internal/adapters/providers/spotify"
	"altune/go-api/internal/search/internal/adapters/providers/theaudiodb"
	"altune/go-api/internal/search/internal/adapters/providers/ytmusic"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/random"
	"fmt"
)

func (t *providerTable) addDeezer(c connections.Clients) {
	adapter := deezer.NewDeezerAdapter(c.ProviderCalls)
	t.add(provider{
		name:          rowDeezer,
		id:            domain.ProviderDeezer,
		search:        adapter,
		artist:        adapter,
		album:         adapter,
		albumFallback: adapter,
		artwork:       []ports.ArtworkLookup{adapter},
		charts:        deezer.NewDeezerAdapter(c.ChartCalls),
		deezerDetails: adapter,
		trackFeatured: adapter,
		lyrics:        deezer.NewDeezerLyricsAdapter(c.ProviderCalls, clock.Now),
		similar:       adapter,
	})
}

func (t *providerTable) addAppleMusic(c connections.Clients) {
	adapter := applemusic.NewAppleMusicAdapter(c.ProviderCalls, clock.Now)
	t.add(provider{name: rowAppleMusic, id: domain.ProviderAppleMusic, search: adapter, searchTimeout: adapter.SearchTimeout(), artist: adapter, album: adapter})
}

func (t *providerTable) addMusicBrainz(userAgent string, c connections.Clients) {
	if userAgent == "" {
		t.add(switchedOff(rowMusicBrainz, "MUSICBRAINZ_USER_AGENT is empty"))
		return
	}
	adapter := musicbrainz.NewMusicBrainzAdapter(c.ProviderCalls, userAgent, clock.Now)
	t.add(provider{
		name:           rowMusicBrainz,
		id:             domain.ProviderMusicBrainz,
		artistAnchor:   adapter,
		enrichment:     adapter,
		featuredSearch: adapter,
		recordings:     adapter,
	})
}

func (t *providerTable) addSoundCloud(c connections.Clients) error {
	adapter, err := soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, soundcloud.NoFallback{}, clock.Now)
	if err != nil {
		return fmt.Errorf("build the soundcloud content adapter: %w", err)
	}
	searcher, err := soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, soundcloud.NewSoundCloudAdapter(), clock.Now)
	if err != nil {
		return fmt.Errorf("build the soundcloud search adapter: %w", err)
	}
	t.add(provider{
		name:          rowSoundCloud,
		id:            domain.ProviderSoundCloud,
		search:        searcher,
		searchTimeout: searcher.SearchTimeout(),
		artist:        adapter,
		album:         adapter,
		related:       adapter,
		artwork:       []ports.ArtworkLookup{adapter},
	})
	return nil
}

func (t *providerTable) addYouTubeMusic(c connections.Clients) {
	adapter := ytmusic.MustNewYouTubeMusicAdapter(c.Transport, clock.Sleep, random.Real)
	t.add(provider{
		name:          rowYouTube,
		id:            domain.ProviderYouTube,
		search:        adapter,
		searchTimeout: adapter.SearchTimeout(),
		artwork:       []ports.ArtworkLookup{ytmusic.MustNewYouTubeMusicArtworkLookup(c.Transport, clock.Sleep, random.Real)},
	})
}

func (t *providerTable) addSpotify(c connections.Clients) {
	adapter := spotify.NewSpotifyAdapter(c.ProviderCalls, clock.Now)
	artwork := spotify.NewSpotifyArtworkLookup(c.ProviderCalls)
	t.add(provider{
		name:          rowSpotify,
		id:            domain.ProviderSpotify,
		search:        adapter,
		searchTimeout: adapter.SearchTimeout(),
		artist:        adapter,
		album:         adapter,
		artworkByID:   []ports.IdentityArtworkLookup{artwork},
	})
}

func (t *providerTable) addITunes(c connections.Clients) {
	adapter := itunes.NewITunesAdapter(c.ProviderCalls)
	t.add(provider{
		name:    rowITunes,
		id:      domain.ProviderITunes,
		album:   adapter,
		artwork: []ports.ArtworkLookup{adapter},
	})
}

func (t *providerTable) addCoverArtArchive(c connections.Clients) {
	byTitle := coverartarchive.NewCoverArtArchiveResolver(c.ProviderCalls)
	byIdentity := coverartarchive.NewCoverArtArchiveIdentityResolver(byTitle)
	t.add(provider{name: rowCoverArtArchive, artwork: []ports.ArtworkLookup{byTitle}, artworkByID: []ports.IdentityArtworkLookup{byIdentity}})
}

func (t *providerTable) addDiscogs(token, userAgent string, c connections.Clients) {
	if token == "" {
		t.add(switchedOff(rowDiscogs, "DISCOGS_TOKEN is empty"))
		return
	}
	adapter := discogs.NewDiscogsAdapter(c.ProviderCalls, token, userAgent)
	t.add(provider{
		name:        rowDiscogs,
		id:          domain.ProviderDiscogs,
		artworkByID: []ports.IdentityArtworkLookup{adapter},
	})
}

func (t *providerTable) addFanartTV(apiKey string, c connections.Clients) {
	if apiKey == "" {
		t.add(switchedOff(rowFanartTV, "FANARTTV_API_KEY is empty"))
		return
	}
	resolver := fanarttv.NewFanartTvArtworkLookup(c.ProviderCalls, apiKey)
	t.add(provider{name: rowFanartTV, artwork: []ports.ArtworkLookup{resolver}})
}

func (t *providerTable) addGenius(accessToken string, c connections.Clients) {
	if accessToken == "" {
		t.add(switchedOff(rowGenius, "GENIUS_ACCESS_TOKEN is empty"))
		return
	}
	t.add(provider{
		name:            rowGenius,
		artwork:         []ports.ArtworkLookup{genius.NewGeniusArtworkLookup(c.ProviderCalls, accessToken)},
		unreleasedSongs: genius.NewSongCatalog(c.ProviderCalls, accessToken),
	})
}

func (t *providerTable) addTheAudioDB(apiKey string, c connections.Clients) {
	if apiKey == "" {
		t.add(switchedOff(rowTheAudioDB, "THEAUDIODB_API_KEY is empty"))
		return
	}
	adapter := theaudiodb.New(c.ProviderCalls, theaudiodb.WithAPIKey(apiKey))
	t.add(provider{name: rowTheAudioDB, id: domain.ProviderTheAudioDB, artwork: []ports.ArtworkLookup{adapter}})
}

func (t *providerTable) addTrackers(apiKey, hubSheetID string, c connections.Clients, st connections.Stores) error {
	if apiKey == "" {
		t.add(switchedOff(rowTrackers, "GOOGLE_SHEETS_API_KEY is empty"))
		return nil
	}
	client, err := fansheets.NewClient(c.ProviderCalls, apiKey, hubSheetID, st.TrackerIndex())
	if err != nil {
		return fmt.Errorf("build the tracker sheet client: %w", err)
	}
	t.add(provider{name: rowTrackers, unreleasedLists: client})
	return nil
}
