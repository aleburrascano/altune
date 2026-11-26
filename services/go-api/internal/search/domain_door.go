package search

import domain "altune/go-api/internal/search/internal/domain"

type (
	InteractionEvent = domain.InteractionEvent
	ProviderName     = domain.ProviderName
	ResultKind       = domain.ResultKind
	ResultKindSet    = domain.ResultKindSet
	SearchQuery      = domain.SearchQuery
	SearchResult     = domain.SearchResult
	ProviderLink     = domain.ProviderLink
)

var (
	AllKinds          = domain.AllKinds
	NewSearchQuery    = domain.NewSearchQuery
	ParseProviderName = domain.ParseProviderName
)

const (
	ProviderDeezer     = domain.ProviderDeezer
	ProviderSoundCloud = domain.ProviderSoundCloud
	ProviderSpotify    = domain.ProviderSpotify
	ProviderYouTube    = domain.ProviderYouTube
	ResultKindAlbum    = domain.ResultKindAlbum
	ResultKindArtist   = domain.ResultKindArtist
	ResultKindTrack    = domain.ResultKindTrack
)
