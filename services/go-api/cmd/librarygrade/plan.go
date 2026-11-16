package main

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	backfillGeneration = 0
	youtubeKeyPrefix   = "youtube:"
	soundCloudHost     = "soundcloud.com"
)

var errUnsupportedSource = errors.New("source is neither YouTube nor SoundCloud")

type authorLookup interface {
	Watch(ctx context.Context, videoID string) (acquisition.YtmWatch, error)
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
	SourceKind         acquisition.SourceKind `json:"source_kind"`
	PipelineGeneration int                    `json:"pipeline_generation"`
	Grade              acquisition.Grade      `json:"grade"`
	BackfillRun        string                 `json:"backfill_run"`
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
		Grade:              acquisition.GradeOf(acquisition.FingerprintOf(acquisition.VerdictKind(row.Verdict)), shared.AcquisitionProvenance(row.Provenance), kind),
		BackfillRun:        runID,
	}, nil
}

func classifySource(ctx context.Context, lookup authorLookup, artist, sourceURL string) (acquisition.SourceKind, error) {
	if sourceURL == "" {
		return acquisition.UserUpload, nil
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

func classifyYouTube(ctx context.Context, lookup authorLookup, artist, videoID string) (acquisition.SourceKind, error) {
	watch, err := lookup.Watch(ctx, videoID)
	switch {
	case err == nil && watch.Type == acquisition.VideoTypeATV:
		return acquisition.ArtTrack, nil
	case err == nil && watch.Type == acquisition.VideoTypeOMV:
		return acquisition.OfficialVideo, nil
	case err != nil && !errors.Is(err, acquisition.ErrNoWatchTrack):
		return "", fmt.Errorf("youtube watch: %w", err)
	}
	author, err := lookup.YouTubeAuthor(ctx, videoID)
	return uploaderKind(artist, author, err)
}

func uploaderKind(artist, author string, lookupErr error) (acquisition.SourceKind, error) {
	if lookupErr != nil {
		return "", lookupErr
	}
	return acquisition.UploaderSourceKind(artist, author), nil
}

func youtubeVideoID(sourceURL string) (string, bool) {
	return strings.CutPrefix(acquisition.SourceKey(sourceURL), youtubeKeyPrefix)
}

func isSoundCloud(sourceURL string) bool {
	u, err := url.Parse(sourceURL)
	if err != nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == soundCloudHost || strings.HasSuffix(host, "."+soundCloudHost)
}
