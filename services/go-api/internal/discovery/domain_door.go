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
	ProviderAppleMusic       = domain.ProviderAppleMusic
	ProviderDeezer           = domain.ProviderDeezer
	ProviderDiscogs          = domain.ProviderDiscogs
	ProviderITunes           = domain.ProviderITunes
	ProviderMusicBrainz      = domain.ProviderMusicBrainz
	ProviderSoundCloud       = domain.ProviderSoundCloud
	ProviderSpotify          = domain.ProviderSpotify
	ProviderTheAudioDB       = domain.ProviderTheAudioDB
	ProviderYouTube          = domain.ProviderYouTube
	ResultKindAlbum          = domain.ResultKindAlbum
	ResultKindArtist         = domain.ResultKindArtist
	ResultKindPlaylist       = domain.ResultKindPlaylist
	ResultKindTrack          = domain.ResultKindTrack
	VocabKindArtist          = domain.VocabKindArtist
	VocabKindTrack           = domain.VocabKindTrack
)

type (
	AudioLink          = domain.AudioLink
	GeniusSongDetail   = domain.GeniusSongDetail
	ProviderStatus     = domain.ProviderStatus
	SearchHistoryEntry = domain.SearchHistoryEntry
	UnreleasedVersion  = domain.UnreleasedVersion
)

const ConfidenceLow = domain.ConfidenceLow

const (
	ProviderStatusError = domain.ProviderStatusError
	ProviderStatusOK    = domain.ProviderStatusOK
)
