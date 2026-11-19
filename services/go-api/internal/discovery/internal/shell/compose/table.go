package compose

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/core/ports"
	"context"
	"slices"
)

type rowName string

const (
	rowDeezer          rowName = "deezer"
	rowAppleMusic      rowName = "applemusic"
	rowMusicBrainz     rowName = "musicbrainz"
	rowLastFM          rowName = "lastfm"
	rowSoundCloud      rowName = "soundcloud"
	rowYouTube         rowName = "youtube"
	rowAmazonMusic     rowName = "amazonmusic"
	rowSpotify         rowName = "spotify"
	rowITunes          rowName = "itunes"
	rowCoverArtArchive rowName = "coverartarchive"
	rowDiscogs         rowName = "discogs"
	rowFanartTV        rowName = "fanarttv"
	rowGenius          rowName = "genius"
	rowTheAudioDB      rowName = "theaudiodb"
	rowTrackers        rowName = "trackers"
)

type job string

const (
	jobSearch     job = "search"
	jobArtist     job = "artist"
	jobAlbum      job = "album"
	jobRelated    job = "related"
	jobArtwork    job = "artwork"
	jobCharts     job = "charts"
	jobDetails    job = "details"
	jobEnrichment job = "enrichment"
	jobFeatured   job = "featured"
	jobLyrics     job = "lyrics"
	jobSimilar    job = "similar"
	jobUnreleased job = "unreleased"
	jobRecordings job = "recordings"
)

const notInSearchAllowlist = "not in DISCOVERY_SEARCH_PROVIDERS"

type trackFeaturedLookup interface {
	ResolveID(ctx context.Context, kind domain.ResultKind, artist, title string) (string, error)
	LookupTrackFeatured(ctx context.Context, trackID string) ([]domain.FeaturedArtist, error)
}

type featuredSearcher interface {
	SearchStructured(ctx context.Context, artist, track string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

type similarCatalog interface {
	ports.AlbumContentProvider
	ports.ArtistContentProvider
}

type provider struct {
	name            rowName
	id              domain.ProviderName
	offReason       string
	searchOffReason string
	search          ports.SearchProvider
	artist          ports.ArtistContentProvider
	artistAnchor    ports.MBDiscographyAnchor
	album           ports.AlbumContentProvider
	albumFallback   ports.SearchProvider
	related         ports.RelatedTracksProvider
	artwork         []ports.ArtworkResolver
	charts          ports.ChartProvider
	deezerDetails   ports.DeezerEnricher
	lastFMDetails   ports.LastFmEnricher
	enrichment      ports.MetadataEnricher
	trackFeatured   trackFeaturedLookup
	featuredSearch  featuredSearcher
	lyrics          ports.LyricsProvider
	similar         similarCatalog
	unreleasedSongs ports.GeniusSongCatalog
	unreleasedLists ports.TrackerSongs
	recordings      RecordingAuthority
}

type providerTable struct {
	providers          []provider
	unknownAllowlisted []string
}

func newProviderTable(s handles.ProviderSettings, c handles.Clients, st handles.Stores) *providerTable {
	t := &providerTable{}
	t.addDeezer(c)
	t.addAppleMusic(s.AppleMusicEnabled, c)
	t.addMusicBrainz(s.MusicBrainzUserAgent, c)
	t.addLastFM(s.LastFMAPIKey, c)
	t.addSoundCloud(s.SoundCloudEnabled, c)
	t.addYouTubeMusic(s.YouTubeMusicEnabled, c)
	t.addAmazonMusic(s.AmazonMusicEnabled, c)
	t.addSpotify(s.SpotifyEnabled, c)
	t.addITunes(c)
	t.addCoverArtArchive(c)
	t.addDiscogs(s.DiscogsToken, s.MusicBrainzUserAgent, c)
	t.addFanartTV(s.FanartTVAPIKey, c)
	t.addGenius(s.GeniusAccessToken, c)
	t.addTheAudioDB(s.TheAudioDBAPIKey, c)
	t.addTrackers(s.GoogleSheetsAPIKey, s.TrackerHubSheetID, c, st)
	t.searchOnlyWith(s.SearchAllowlist)
	return t
}

func (t *providerTable) add(p provider) {
	t.providers = append(t.providers, p)
}

func switchedOff(name rowName, reason string) provider {
	return provider{name: name, offReason: reason}
}

func (t *providerTable) named(name rowName) provider {
	for _, p := range t.providers {
		if p.name == name {
			return p
		}
	}
	return provider{}
}

func (t *providerTable) searchOnlyWith(allowlist []string) {
	if len(allowlist) == 0 {
		return
	}
	t.unknownAllowlisted = t.entriesNamingNoSearcher(allowlist)
	for i := range t.providers {
		t.providers[i].searchOnlyIfListed(allowlist)
	}
}

func (t *providerTable) entriesNamingNoSearcher(allowlist []string) []string {
	var unknown []string
	for _, entry := range allowlist {
		if !slices.ContainsFunc(t.providers, func(p provider) bool { return p.mayBeAllowlistedAs(entry) }) {
			unknown = append(unknown, entry)
		}
	}
	return unknown
}

func (p provider) mayBeAllowlistedAs(entry string) bool {
	return string(p.name) == entry && (p.search != nil || p.offReason != "")
}

func (p *provider) searchOnlyIfListed(allowlist []string) {
	if p.search == nil || slices.Contains(allowlist, p.search.Name().String()) {
		return
	}
	p.search = nil
	if len(p.jobs()) == 0 {
		p.offReason = notInSearchAllowlist
		return
	}
	p.searchOffReason = notInSearchAllowlist
}

type jobSlot struct {
	job    job
	filled bool
}

func (p provider) catalogSlots() []jobSlot {
	return []jobSlot{
		{jobSearch, p.search != nil},
		{jobArtist, p.artist != nil},
		{jobArtist, p.artistAnchor != nil},
		{jobAlbum, p.album != nil},
		{jobAlbum, p.albumFallback != nil},
		{jobRelated, p.related != nil},
		{jobArtwork, len(p.artwork) > 0},
		{jobCharts, p.charts != nil},
	}
}

func (p provider) detailSlots() []jobSlot {
	return []jobSlot{
		{jobDetails, p.deezerDetails != nil},
		{jobDetails, p.lastFMDetails != nil},
		{jobEnrichment, p.enrichment != nil},
		{jobFeatured, p.trackFeatured != nil},
		{jobFeatured, p.featuredSearch != nil},
		{jobLyrics, p.lyrics != nil},
		{jobSimilar, p.similar != nil},
		{jobUnreleased, p.unreleasedSongs != nil},
		{jobUnreleased, p.unreleasedLists != nil},
		{jobRecordings, p.recordings != nil},
	}
}

func (p provider) jobs() []job {
	var jobs []job
	for _, slot := range append(p.catalogSlots(), p.detailSlots()...) {
		if slot.filled && !slices.Contains(jobs, slot.job) {
			jobs = append(jobs, slot.job)
		}
	}
	return jobs
}
