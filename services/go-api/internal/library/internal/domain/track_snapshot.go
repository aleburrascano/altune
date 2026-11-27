package domain

import (
	"altune/go-api/internal/shared/ids"
	"encoding/json"
	"slices"
	"time"
)

type TrackSnapshot struct {
	ID                     TrackId
	UserId                 ids.UserId
	Title                  string
	Artist                 string
	Album                  string
	DurationSeconds        *float64
	AddedAt                time.Time
	ArtworkURL             *string
	DownloadStatus         DownloadStatus
	DedupKey               string
	IdempotencyKey         *string
	Year                   *int
	Genre                  *string
	TrackNumber            *int
	AlbumArtist            *string
	ISRC                   *string
	AudioRef               *string
	AudioVersion           string
	FailureReason          *string
	FeaturedArtists        []FeaturedArtist
	AcquisitionProvenance  *string
	AudioSourceURL         *string
	ConfidenceScore        *float64
	EvidenceJSON           json.RawMessage
	CatalogDurationSeconds *float64
	CatalogMBID            *string
	CatalogExplicit        *bool
	CatalogSources         []CatalogSource
	AudioLengthMismatch    bool
	AcquisitionStartedAt   *time.Time
	Version                int
	AcquisitionFence       int
}

func RestoreTrack(s TrackSnapshot) *Track {
	return &Track{
		id:                     s.ID,
		userId:                 s.UserId,
		title:                  s.Title,
		artist:                 s.Artist,
		album:                  s.Album,
		durationSeconds:        clonePtr(s.DurationSeconds),
		addedAt:                s.AddedAt,
		artworkURL:             clonePtr(s.ArtworkURL),
		downloadStatus:         s.DownloadStatus,
		dedupKey:               s.DedupKey,
		idempotencyKey:         clonePtr(s.IdempotencyKey),
		year:                   clonePtr(s.Year),
		genre:                  clonePtr(s.Genre),
		trackNumber:            clonePtr(s.TrackNumber),
		albumArtist:            clonePtr(s.AlbumArtist),
		isrc:                   clonePtr(s.ISRC),
		audioRef:               clonePtr(s.AudioRef),
		audioVersion:           s.AudioVersion,
		failureReason:          clonePtr(s.FailureReason),
		featuredArtists:        slices.Clone(s.FeaturedArtists),
		acquisitionProvenance:  clonePtr(s.AcquisitionProvenance),
		audioSourceURL:         clonePtr(s.AudioSourceURL),
		confidenceScore:        clonePtr(s.ConfidenceScore),
		evidenceJSON:           slices.Clone(s.EvidenceJSON),
		catalogDurationSeconds: clonePtr(s.CatalogDurationSeconds),
		catalogMBID:            clonePtr(s.CatalogMBID),
		catalogExplicit:        clonePtr(s.CatalogExplicit),
		catalogSources:         slices.Clone(s.CatalogSources),
		audioLengthMismatch:    s.AudioLengthMismatch,
		acquisitionStartedAt:   clonePtr(s.AcquisitionStartedAt),
		version:                s.Version,
		acquisitionFence:       s.AcquisitionFence,
	}
}

func (t *Track) Snapshot() TrackSnapshot {
	return TrackSnapshot{
		ID:                     t.id,
		UserId:                 t.userId,
		Title:                  t.title,
		Artist:                 t.artist,
		Album:                  t.album,
		DurationSeconds:        t.DurationSeconds(),
		AddedAt:                t.addedAt,
		ArtworkURL:             t.ArtworkURL(),
		DownloadStatus:         t.downloadStatus,
		DedupKey:               t.dedupKey,
		IdempotencyKey:         t.IdempotencyKey(),
		Year:                   t.Year(),
		Genre:                  t.Genre(),
		TrackNumber:            t.TrackNumber(),
		AlbumArtist:            t.AlbumArtist(),
		ISRC:                   t.ISRC(),
		AudioRef:               t.AudioRef(),
		AudioVersion:           t.audioVersion,
		FailureReason:          t.FailureReason(),
		FeaturedArtists:        t.FeaturedArtists(),
		AcquisitionProvenance:  t.AcquisitionProvenance(),
		AudioSourceURL:         t.AudioSourceURL(),
		ConfidenceScore:        t.ConfidenceScore(),
		EvidenceJSON:           t.EvidenceJSON(),
		CatalogDurationSeconds: t.CatalogDurationSeconds(),
		CatalogMBID:            t.CatalogMBID(),
		CatalogExplicit:        t.CatalogExplicit(),
		CatalogSources:         t.CatalogSources(),
		AudioLengthMismatch:    t.audioLengthMismatch,
		AcquisitionStartedAt:   t.AcquisitionStartedAt(),
		Version:                t.version,
		AcquisitionFence:       t.acquisitionFence,
	}
}
