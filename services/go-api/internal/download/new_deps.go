package download

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"net/http"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

var ErrTransportRequired = errors.New("download: Deps.Transport is required, so acquisition calls never fall back to an unpaced transport")

type IdentifierHealth interface {
	ports.AudioIdentifier
	Verified() bool
}

type SourceOrder struct {
	First    []ports.AudioSource
	Fallback []ports.AudioSource
}

func (t SourceOrder) All() []ports.AudioSource {
	return append(append([]ports.AudioSource{}, t.First...), t.Fallback...)
}

type SourceDeps struct {
	Config    *config.Config
	Redis     *goredis.Client
	Transport http.RoundTripper
	NewClient func(timeout time.Duration, transport http.RoundTripper) *http.Client
}

type Deps struct {
	Config    *config.Config
	Transport http.RoundTripper
	Database  *persistence.Database
	Events    events.Publisher
	NewClient func(timeout time.Duration, transport http.RoundTripper) *http.Client

	Ledger       ports.LibraryTracks
	AudioStore   ports.AudioWriter
	StoredAudio  ports.StoredAudioReader
	Orphans      ports.OrphanedAudioRecorder
	Sources      SourceOrder
	Verification ports.Verification

	RecordingResolver ports.RecordingResolver
	RecordingLookup   ports.RecordingLookup
	Rerecordings      ports.RerecordingFinder

	TrackIdentity   func(identifier ports.AudioIdentifier) IdentifierHealth
	InPlaceRecorder func(state InPlaceState) ports.InPlaceStore
}

type InPlaceState interface {
	RecordInPlaceState(ctx context.Context, trackID ids.TrackId, generation int, basis string, bitErrorRate float64) error
	NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error)
}
