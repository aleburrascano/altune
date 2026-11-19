package compose

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/providercall"
	"context"
	"slices"
)

type rowName string

const (
	rowDeezer          rowName = "deezer"
	rowAppleMusic      rowName = "applemusic"
	rowMusicBrainz     rowName = "musicbrainz"
	rowSoundCloud      rowName = "soundcloud"
	rowYouTube         rowName = "youtube"
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
	jobConsensus  job = "consensus"
	jobDetails    job = "details"
	jobEnrichment job = "enrichment"
	jobFeatured   job = "featured"
	jobLyrics     job = "lyrics"
	jobSimilar    job = "similar"
	jobUnreleased job = "unreleased"
	jobRecordings job = "recordings"
)

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
	search          ports.SearchProvider
	artist          ports.ArtistContentProvider
	artistAnchor    ports.MBDiscographyAnchor
	album           ports.AlbumContentProvider
	albumFallback   ports.SearchProvider
	related         ports.RelatedTracksProvider
	artwork         []ports.ArtworkResolver
	charts          ports.ChartProvider
	consensus       providercall.ConsensusProvider
	deezerDetails   ports.DeezerEnricher
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
	providers []provider
}

func newProviderTable(s handles.ProviderSettings, c handles.Clients, st handles.Stores) *providerTable {
	t := &providerTable{}
	t.addDeezer(c)
	t.addAppleMusic(c)
	t.addMusicBrainz(s.MusicBrainzUserAgent, c)
	t.addSoundCloud(c)
	t.addYouTubeMusic(c)
	t.addSpotify(c)
	t.addITunes(c)
	t.addCoverArtArchive(c)
	t.addDiscogs(s.DiscogsToken, s.MusicBrainzUserAgent, c)
	t.addFanartTV(s.FanartTVAPIKey, c)
	t.addGenius(s.GeniusAccessToken, c)
	t.addTheAudioDB(s.TheAudioDBAPIKey, c)
	t.addTrackers(s.GoogleSheetsAPIKey, s.TrackerHubSheetID, c, st)
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
		{jobConsensus, p.consensus.Fetcher != nil},
	}
}

func (p provider) detailSlots() []jobSlot {
	return []jobSlot{
		{jobDetails, p.deezerDetails != nil},
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
