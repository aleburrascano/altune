package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	acqdomain "altune/go-api/internal/acquisition/domain"
)

const (
	backfillGeneration = 0
	youtubeKeyPrefix   = "youtube:"
	soundCloudHost     = "soundcloud.com"
)

var errUnsupportedSource = errors.New("source is neither YouTube nor SoundCloud")

type authorLookup interface {
	Watch(ctx context.Context, videoID string) (ytmsearch.Watch, error)
	YouTubeAuthor(ctx context.Context, videoID string) (string, error)
	SoundCloudAuthor(ctx context.Context, trackURL string) (string, error)
}

type trackRow struct {
	ID         string
	UserID     string
	Artist     string
	SourceURL  string
	Provenance string
	Verdict    string
	Version    int
}

type patch struct {
	SourceKind         grade.SourceKind `json:"source_kind"`
	PipelineGeneration int              `json:"pipeline_generation"`
	Grade              grade.Grade      `json:"grade"`
	BackfillRun        string           `json:"backfill_run"`
}

func (p patch) json() ([]byte, error) { return json.Marshal(p) }

func planTrack(ctx context.Context, lookup authorLookup, row trackRow, runID string) (patch, error) {
	kind, err := classifySource(ctx, lookup, row.Artist, row.SourceURL)
	if err != nil {
		return patch{}, err
	}
	return patch{
		SourceKind:         kind,
		PipelineGeneration: backfillGeneration,
		Grade:              grade.Of(service.FingerprintOf(service.VerdictKind(row.Verdict)), shared.AcquisitionProvenance(row.Provenance), kind),
		BackfillRun:        runID,
	}, nil
}

func classifySource(ctx context.Context, lookup authorLookup, artist, sourceURL string) (grade.SourceKind, error) {
	if sourceURL == "" {
		return grade.UserUpload, nil
	}
	if videoID, isYouTube := youtubeVideoID(sourceURL); isYouTube {
		return classifyYouTube(ctx, lookup, artist, videoID)
	}
	if isSoundCloud(sourceURL) {
		author, err := lookup.SoundCloudAuthor(ctx, sourceURL)
		return uploaderKind(artist, author, err)
	}
	return "", errUnsupportedSource
}

func classifyYouTube(ctx context.Context, lookup authorLookup, artist, videoID string) (grade.SourceKind, error) {
	watch, err := lookup.Watch(ctx, videoID)
	switch {
	case err == nil && watch.Type == ytmsearch.VideoTypeATV:
		return grade.ArtTrack, nil
	case err == nil && watch.Type == ytmsearch.VideoTypeOMV:
		return grade.OfficialVideo, nil
	case err != nil && !errors.Is(err, ytmsearch.ErrNoWatchTrack):
		return "", fmt.Errorf("youtube watch: %w", err)
	}
	author, err := lookup.YouTubeAuthor(ctx, videoID)
	return uploaderKind(artist, author, err)
}

func uploaderKind(artist, author string, lookupErr error) (grade.SourceKind, error) {
	if lookupErr != nil {
		return "", lookupErr
	}
	return service.UploaderSourceKind(artist, author), nil
}

func youtubeVideoID(sourceURL string) (string, bool) {
	return strings.CutPrefix(acqdomain.SourceKey(sourceURL), youtubeKeyPrefix)
}

func isSoundCloud(sourceURL string) bool {
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == soundCloudHost || strings.HasSuffix(host, "."+soundCloudHost)
}
