package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/fakesources"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
)

type (
	AudioSource    = ports.AudioSource
	AudioCandidate = domain.AudioCandidate
	FindRequest    = domain.FindRequest
	FetchedAudio   = domain.FetchedAudio
	SourceName     = domain.SourceName
	SourceOrder    = ports.SourceOrder
)

const (
	ArtTrackURL    = fakesources.ArtTrackURL
	JobKindAcquire = domain.JobKindAcquire
	JobKindReplace = domain.JobKindReplace
)

var (
	NewArtTrackSource = fakesources.NewArtTrackSource
	NewUploadSource   = fakesources.NewUploadSource
	ErrNoJobAvailable = domain.ErrNoJobAvailable
)
