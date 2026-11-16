package discovery

import domain "altune/go-api/internal/discovery/domain"

type (
	InteractionEvent = domain.InteractionEvent
	MBEnrichment     = domain.MBEnrichment
	ProviderKey      = domain.ProviderKey
	ProviderName     = domain.ProviderName
	ResultKind       = domain.ResultKind
	SearchQuery      = domain.SearchQuery
	SearchResult     = domain.SearchResult
	SourceRef        = domain.SourceRef
	UnreleasedSong   = domain.UnreleasedSong
	VocabularyEntry  = domain.VocabularyEntry
	VocabularyKind   = domain.VocabularyKind
)

var (
	AllKinds          = domain.AllKinds
	CleanQuery        = domain.CleanQuery
	EmptyEnrichment   = domain.EmptyEnrichment
	NewSearchQuery    = domain.NewSearchQuery
	ParseProviderName = domain.ParseProviderName
	ParseResultKind   = domain.ParseResultKind
)

const (
	EventTypeSearchPerformed = domain.EventTypeSearchPerformed
	ProviderAmazonMusic      = domain.ProviderAmazonMusic
	ProviderAppleMusic       = domain.ProviderAppleMusic
	ProviderDeezer           = domain.ProviderDeezer
	ProviderDiscogs          = domain.ProviderDiscogs
	ProviderITunes           = domain.ProviderITunes
	ProviderKeyDiscogs       = domain.ProviderKeyDiscogs
	ProviderKeyITunes        = domain.ProviderKeyITunes
	ProviderKeyLastFM        = domain.ProviderKeyLastFM
	ProviderKeyMusicBrainz   = domain.ProviderKeyMusicBrainz
	ProviderKeySoundCloud    = domain.ProviderKeySoundCloud
	ProviderKeyYTMusic       = domain.ProviderKeyYTMusic
	ProviderLastFM           = domain.ProviderLastFM
	ProviderMusicBrainz      = domain.ProviderMusicBrainz
	ProviderSoundCloud       = domain.ProviderSoundCloud
	ProviderSpotify          = domain.ProviderSpotify
	ProviderYouTube          = domain.ProviderYouTube
	ResultKindAlbum          = domain.ResultKindAlbum
	ResultKindArtist         = domain.ResultKindArtist
	ResultKindTrack          = domain.ResultKindTrack
	VocabKindArtist          = domain.VocabKindArtist
	VocabKindTrack           = domain.VocabKindTrack
)
