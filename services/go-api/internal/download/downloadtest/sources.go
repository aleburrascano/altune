package downloadtest

import (
	"altune/go-api/internal/download/internal/adapters/fakesources"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
)

type (
	AudioSource    = ports.AudioSource
	AudioCandidate = ports.AudioCandidate
	FindRequest    = ports.FindRequest
	FetchedAudio   = ports.FetchedAudio
	SourceName     = domain.SourceName
	SourceOrder    = ports.SourceOrder
)

const (
	ArtTrackURL    = fakesources.ArtTrackURL
	JobKindAcquire = ports.JobKindAcquire
	JobKindReplace = ports.JobKindReplace
)

var (
	NewArtTrackSource = fakesources.NewArtTrackSource
	NewUploadSource   = fakesources.NewUploadSource
	ErrNoJobAvailable = domain.ErrNoJobAvailable
)
