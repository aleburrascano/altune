package downloadsetup

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/app/bridges/downloadfromsearch"
	"altune/go-api/internal/app/fingerprintstatus"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/platform/monitoring"
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/shared/config"
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Deps struct {
	Config       *config.Config
	Transport    http.RoundTripper
	Pool         *pgxpool.Pool
	DBMetrics    library.DBCallMetrics
	Events       *monitoring.EventTap
	Recording    downloadfromsearch.RecordingWiring
	Ledger       download.TrackLedgerPort
	AudioStore   library.AudioStore
	Sources      SourceOrder
	Verification download.Verification
}

var ErrTransportRequired = download.ErrTransportRequired

func NewScheduler(ctx context.Context, d Deps) *download.BackgroundAcquisitionScheduler {
	if d.Transport == nil {
		panic(ErrTransportRequired)
	}
	return download.New(ctx, download.Deps{
		Config:            d.Config,
		Transport:         d.Transport,
		Database:          download.NewPostgresDatabase(d.Pool),
		Events:            d.Events,
		NewClient:         newClient,
		Ledger:            d.Ledger,
		AudioStore:        d.AudioStore,
		StoredAudio:       downloadfromlibrary.NewStoredAudioReader(d.AudioStore),
		Orphans:           downloadfromlibrary.NewOrphanedAudioRecorder(library.NewPostgresOrphanedAudioRepository(d.Pool, d.DBMetrics)),
		Sources:           d.Sources,
		Verification:      d.Verification,
		RecordingResolver: d.Recording.Resolver,
		RecordingLookup:   d.Recording.Lookup,
		Rerecordings:      d.Recording.Rerecordings,
		TrackIdentity: func(identifier download.AudioIdentifier) download.IdentifierHealth {
			return fingerprintstatus.NewTracker(identifier)
		},
		InPlaceRecorder: func(state download.InPlaceState) download.InPlaceStore {
			return downloadfromlibrary.NewInPlaceRecorder(library.NewPostgresTrackRepository(d.Pool, d.DBMetrics), state)
		},
	})
}

func newClient(timeout time.Duration, transport http.RoundTripper) *http.Client {
	return outbound.MustNewClient(timeout, transport)
}
