package compose

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/adapters/providers/applemusic"
	"altune/go-api/internal/discovery/internal/adapters/providers/coverartarchive"
	"altune/go-api/internal/discovery/internal/adapters/providers/deezer"
	"altune/go-api/internal/discovery/internal/adapters/providers/discogs"
	"altune/go-api/internal/discovery/internal/adapters/providers/fanarttv"
	"altune/go-api/internal/discovery/internal/adapters/providers/genius"
	"altune/go-api/internal/discovery/internal/adapters/providers/itunes"
	"altune/go-api/internal/discovery/internal/adapters/providers/musicbrainz"
	"altune/go-api/internal/discovery/internal/adapters/providers/soundcloud"
	"altune/go-api/internal/discovery/internal/adapters/providers/spotify"
	"altune/go-api/internal/discovery/internal/adapters/providers/theaudiodb"
	"altune/go-api/internal/discovery/internal/adapters/providers/trackers"
	"altune/go-api/internal/discovery/internal/adapters/providers/ytmusic"
	"altune/go-api/internal/discovery/internal/core/ports"
	"time"
)

func (t *providerTable) addDeezer(c handles.Clients) {
	adapter := deezer.NewDeezerAdapter(c.ProviderCalls)
	t.add(provider{
		name:          rowDeezer,
		id:            domain.ProviderDeezer,
		search:        adapter,
		artist:        adapter,
		album:         adapter,
		albumFallback: adapter,
		artwork:       []ports.ArtworkResolver{adapter},
		charts:        deezer.NewDeezerAdapter(c.ChartCalls),
		deezerDetails: adapter,
		trackFeatured: adapter,
		lyrics:        deezer.NewDeezerLyricsAdapter(c.ProviderCalls),
		similar:       adapter,
	})
}

func (t *providerTable) addAppleMusic(c handles.Clients) {
	adapter := applemusic.NewAppleMusicAdapter(c.ProviderCalls)
	t.add(provider{name: rowAppleMusic, id: domain.ProviderAppleMusic, search: adapter, artist: adapter, album: adapter})
}

func (t *providerTable) addMusicBrainz(userAgent string, c handles.Clients) {
	if userAgent == "" {
		t.add(switchedOff(rowMusicBrainz, "MUSICBRAINZ_USER_AGENT is empty"))
		return
	}
	adapter := musicbrainz.NewMusicBrainzAdapter(c.ProviderCalls, userAgent)
	t.add(provider{
		name:           rowMusicBrainz,
		id:             domain.ProviderMusicBrainz,
		artistAnchor:   adapter,
		enrichment:     adapter,
		featuredSearch: adapter,
		recordings:     adapter,
	})
}

func (t *providerTable) addSoundCloud(c handles.Clients) {
	adapter := soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, nil)
	t.add(provider{
		name:    rowSoundCloud,
		id:      domain.ProviderSoundCloud,
		search:  soundcloud.NewSoundCloudAPIAdapter(c.ProviderCalls, soundcloud.NewSoundCloudAdapter()),
		artist:  adapter,
		album:   adapter,
		related: adapter,
		artwork: []ports.ArtworkResolver{adapter},
	})
}

func (t *providerTable) addYouTubeMusic(c handles.Clients) {
	adapter := ytmusic.NewYouTubeMusicAdapter(c.Transport)
	t.add(provider{
		name:    rowYouTube,
		id:      domain.ProviderYouTube,
		search:  adapter,
		artwork: []ports.ArtworkResolver{ytmusic.NewYouTubeMusicArtworkResolver(c.Transport)},
	})
}

func (t *providerTable) addSpotify(c handles.Clients) {
	adapter := spotify.NewSpotifyAdapter(c.ProviderCalls, time.Now)
	t.add(provider{
		name:    rowSpotify,
		id:      domain.ProviderSpotify,
		search:  adapter,
		artist:  adapter,
		album:   adapter,
		artwork: []ports.ArtworkResolver{spotify.NewSpotifyArtworkResolver(c.ProviderCalls)},
	})
}

func (t *providerTable) addITunes(c handles.Clients) {
	adapter := itunes.NewITunesAdapter(c.ProviderCalls)
	t.add(provider{
		name:    rowITunes,
		id:      domain.ProviderITunes,
		album:   adapter,
		artwork: []ports.ArtworkResolver{adapter},
	})
}

func (t *providerTable) addCoverArtArchive(c handles.Clients) {
	byTitle := coverartarchive.NewCoverArtArchiveResolver(c.ProviderCalls)
	byIdentity := coverartarchive.NewCoverArtArchiveIdentityResolver(byTitle)
	t.add(provider{name: rowCoverArtArchive, artwork: []ports.ArtworkResolver{byTitle, byIdentity}})
}

func (t *providerTable) addDiscogs(token, userAgent string, c handles.Clients) {
	if token == "" {
		t.add(switchedOff(rowDiscogs, "DISCOGS_TOKEN is empty"))
		return
	}
	adapter := discogs.NewDiscogsAdapter(c.ProviderCalls, token, userAgent)
	t.add(provider{
		name:    rowDiscogs,
		id:      domain.ProviderDiscogs,
		artwork: []ports.ArtworkResolver{adapter},
	})
}

func (t *providerTable) addFanartTV(apiKey string, c handles.Clients) {
	if apiKey == "" {
		t.add(switchedOff(rowFanartTV, "FANARTTV_API_KEY is empty"))
		return
	}
	resolver := fanarttv.NewFanartTvArtworkResolver(c.ProviderCalls, apiKey)
	t.add(provider{name: rowFanartTV, artwork: []ports.ArtworkResolver{resolver}})
}

func (t *providerTable) addGenius(accessToken string, c handles.Clients) {
	if accessToken == "" {
		t.add(switchedOff(rowGenius, "GENIUS_ACCESS_TOKEN is empty"))
		return
	}
	t.add(provider{
		name:            rowGenius,
		artwork:         []ports.ArtworkResolver{genius.NewGeniusArtworkResolver(c.ProviderCalls, accessToken)},
		unreleasedSongs: genius.NewSongCatalog(c.ProviderCalls, accessToken),
	})
}

func (t *providerTable) addTheAudioDB(apiKey string, c handles.Clients) {
	if apiKey == "" {
		t.add(switchedOff(rowTheAudioDB, "THEAUDIODB_API_KEY is empty"))
		return
	}
	adapter := theaudiodb.New(c.ProviderCalls, theaudiodb.WithAPIKey(apiKey))
	t.add(provider{name: rowTheAudioDB, id: domain.ProviderTheAudioDB, artwork: []ports.ArtworkResolver{adapter}})
}

func (t *providerTable) addTrackers(apiKey, hubSheetID string, c handles.Clients, st handles.Stores) {
	if apiKey == "" {
		t.add(switchedOff(rowTrackers, "GOOGLE_SHEETS_API_KEY is empty"))
		return
	}
	t.add(provider{name: rowTrackers, unreleasedLists: trackers.NewClient(c.ProviderCalls, apiKey, hubSheetID, st.TrackerIndex())})
}
