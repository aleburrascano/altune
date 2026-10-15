package main

import (
	"altune/go-api/internal/acquisition/adapters/ytmsearch"
	"context"
	"fmt"
	"slices"
)

const topResults = 5

type songSearch interface {
	SearchSongs(ctx context.Context, query string) ([]ytmsearch.Song, error)
}

type outcomeKind string

const (
	outcomeFound     outcomeKind = "found"
	outcomeAmbiguous outcomeKind = "ambiguous"
	outcomeNone      outcomeKind = "none"
	outcomeErrored   outcomeKind = "errored"
)

type outcome struct {
	Target   target
	Kind     outcomeKind
	VideoID  string
	VideoIDs []string
	Reasons  []rejection
	Err      error
}

func (t target) queries() []string {
	queries := []string{t.Artist + " " + t.Title}
	if t.ISRC != "" {
		queries = slices.Insert(queries, 0, t.ISRC)
	}
	return queries
}

func find(ctx context.Context, search songSearch, t target) outcome {
	result := outcome{Target: t, Kind: outcomeNone}
	for _, query := range t.queries() {
		songs, err := search.SearchSongs(ctx, query)
		if err != nil {
			return outcome{Target: t, Kind: outcomeErrored, Err: fmt.Errorf("search %q: %w", query, err)}
		}
		videoIDs, reasons := t.screen(songs[:min(len(songs), topResults)])
		result.Reasons = append(result.Reasons, reasons...)
		if len(videoIDs) > 0 {
			return t.decided(videoIDs)
		}
	}
	return result
}

func (t target) screen(songs []ytmsearch.Song) ([]string, []rejection) {
	var videoIDs []string
	var reasons []rejection
	for _, song := range songs {
		reason := t.judge(song)
		if reason != accepted {
			reasons = append(reasons, reason)
			continue
		}
		if !slices.Contains(videoIDs, song.VideoID) {
			videoIDs = append(videoIDs, song.VideoID)
		}
	}
	return videoIDs, reasons
}

func (t target) decided(videoIDs []string) outcome {
	if len(videoIDs) > 1 {
		return outcome{Target: t, Kind: outcomeAmbiguous, VideoIDs: videoIDs}
	}
	return outcome{Target: t, Kind: outcomeFound, VideoID: videoIDs[0], VideoIDs: videoIDs}
}
