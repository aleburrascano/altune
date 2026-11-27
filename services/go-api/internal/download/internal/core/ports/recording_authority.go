package ports

import (
	"altune/go-api/internal/shared/songfields"
	"context"
)

type RecordingSearch interface {
	SearchRecordings(ctx context.Context, query string, limit int) ([]songfields.RecordingIdentity, error)
}

type ISRCRecording struct {
	MBID     string
	Duration float64
}

type ISRCLookup interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]ISRCRecording, error)
}

type WorkRecording struct {
	MBID           string
	Title          string
	Disambiguation string
	ArtistIDs      []string
	Live           bool
}

type RecordingRelease struct {
	Title  string
	Date   string
	Status string
}

type RecordingDetail struct {
	MBID             string
	Title            string
	ArtistIDs        []string
	ISRCs            []string
	Duration         float64
	FirstReleaseDate string
	Releases         []RecordingRelease
}

type RerecordingLookup interface {
	WorkRecordings(ctx context.Context, recordingMBID string) ([]WorkRecording, error)
	RecordingDetail(ctx context.Context, mbid string) (RecordingDetail, bool, error)
}
