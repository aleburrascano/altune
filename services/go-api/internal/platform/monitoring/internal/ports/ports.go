package ports

import (
	"altune/go-api/internal/platform/monitoring/internal/domain"
	"context"
	"errors"
	"fmt"
	"time"
)

type AcquisitionTools struct {
	Ffprobe bool
	Ffmpeg  bool
	Fpcalc  bool
	YtDlp   bool
}

type AcquisitionJob struct {
	TrackID        string
	Title          string
	Artist         string
	Album          string
	SourceURL      string
	ResolvedSource string
	State          string
	Stage          string
	ScheduledAt    time.Time
	ElapsedMs      int64
	Reason         string
	Provenance     string
}

type DownloadStatusReport struct {
	InFlight         int
	Succeeded        uint64
	Failed           uint64
	Rejected         uint64
	Paused           bool
	QueueDepth       int
	QueueCapacity    int
	OldestPendingAge time.Duration
	QueueDepthKnown  bool
	Tools            AcquisitionTools
	ActiveJobs       []AcquisitionJob
	Recent           []AcquisitionJob
}

type AcquisitionReader interface {
	Status(context.Context) DownloadStatusReport
}

type AlertNotifier interface {
	Notify(ctx context.Context, a domain.Alert) error
	Silent() bool
}

type ClientEventStore interface {
	Append(ctx context.Context, event domain.ClientEvent) error
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
	Prune(ctx context.Context, now time.Time) (int64, error)
}

type DiscographyGroupBy string

const (
	GroupByArtist            DiscographyGroupBy = "artist"
	GroupByProvider          DiscographyGroupBy = "provider"
	GroupByContaminationBand DiscographyGroupBy = "contamination_band"
)

var ErrUnknownDiscographyGroupBy = errors.New("unknown discography grouping")

func ParseDiscographyGroupBy(raw string) (DiscographyGroupBy, error) {
	switch groupBy := DiscographyGroupBy(raw); groupBy {
	case GroupByArtist, GroupByProvider, GroupByContaminationBand:
		return groupBy, nil
	default:
		return "", fmt.Errorf("%w: %q", ErrUnknownDiscographyGroupBy, raw)
	}
}

type DiscographyCase struct {
	ArtistRef          string
	Releases           int
	SingleProvider     int
	SingleProviderNoID int
	ProviderCounts     map[string]int
	LastSeen           time.Time
}

type DiscographySuspectRate struct {
	Rate       float64
	LastSample time.Time
}

type DiscographyQualityReader interface {
	DiscographyQuality(ctx context.Context, since time.Time, groupBy DiscographyGroupBy, limit int) ([]DiscographyCase, error)
	SuspectRate(ctx context.Context, since time.Time) (DiscographySuspectRate, error)
}
