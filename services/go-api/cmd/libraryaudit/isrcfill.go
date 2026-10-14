package main

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
)

const (
	sourceDeezer      = "deezer"
	sourceMusicBrainz = "musicbrainz"
)

type albumTracklist interface {
	AlbumTracks(ctx context.Context, artist, album string) ([]catalogTrack, error)
}

func albumTracklistCandidates(ctx context.Context, cat catalog, lib libraryTrack) ([]catalogTrack, error) {
	albums, ok := cat.(albumTracklist)
	if !ok || strings.TrimSpace(lib.Album) == "" {
		return nil, nil
	}
	tracks, err := albums.AlbumTracks(ctx, lib.Artist, lib.Album)
	if err != nil {
		return nil, err
	}
	return fittingCandidates(lib, tracks), nil
}

type recordSource int

const (
	recordFromISRCLookup recordSource = iota
	recordFromLibraryAlbum
)

func catalogRecord(ctx context.Context, cat catalog, lib libraryTrack) (catalogTrack, recordSource, error) {
	found, err := cat.TrackByISRC(ctx, lib.ISRC)
	if err != nil {
		return catalogTrack{}, recordFromISRCLookup, err
	}
	onAlbum, err := albumTrackWithISRC(ctx, cat, lib)
	if err != nil {
		return catalogTrack{}, recordFromISRCLookup, err
	}
	if onAlbum != nil {
		return *onAlbum, recordFromLibraryAlbum, nil
	}
	return found, recordFromISRCLookup, nil
}

func albumTrackWithISRC(ctx context.Context, cat catalog, lib libraryTrack) (*catalogTrack, error) {
	candidates, err := albumTracklistCandidates(ctx, cat, lib)
	if err != nil {
		return nil, err
	}
	for _, candidate := range candidates {
		full, err := cat.TrackByID(ctx, candidate.ID)
		if err != nil {
			return nil, err
		}
		if canonicalISRC(full.ISRC) == canonicalISRC(lib.ISRC) {
			return &full, nil
		}
	}
	return nil, nil
}

type isrcResolution struct {
	findings []finding
	reason   string
}

func resolveMissingISRC(ctx context.Context, cat catalog, lib libraryTrack) (isrcResolution, error) {
	candidates, err := cat.Search(ctx, lib.Artist, lib.Title)
	if err != nil {
		return isrcResolution{}, ignoreUnblocked(err)
	}
	fitting := fittingCandidates(lib, candidates)
	if len(fitting) == 0 {
		if fitting, err = albumTracklistCandidates(ctx, cat, lib); err != nil {
			return isrcResolution{}, ignoreUnblocked(err)
		}
	}
	switch len(fitting) {
	case 0:
		return isrcResolution{reason: "deezer search has no same-song candidate on the library album"}, nil
	case 1:
		return resolveFromCandidate(ctx, cat, lib, fitting[0])
	default:
		return isrcResolution{
			findings: []finding{about(lib, categoryISRCAmbiguous, ambiguousDetail(fitting))},
			reason:   "deezer search is ambiguous",
		}, nil
	}
}

func resolveFromCandidate(ctx context.Context, cat catalog, lib libraryTrack, candidate catalogTrack) (isrcResolution, error) {
	full, err := cat.TrackByID(ctx, candidate.ID)
	if err != nil {
		return isrcResolution{}, ignoreUnblocked(err)
	}
	if full.ISRC == "" {
		return isrcResolution{reason: "deezer candidate carries no ISRC"}, nil
	}
	fillable := about(lib, categoryISRCFillable, full.ISRC)
	agreed, reason, err := musicBrainzListsISRC(ctx, cat, lib, candidate, full.ISRC)
	if err != nil {
		return isrcResolution{}, err
	}
	if agreed {
		fillable.Fix = fixing(lib, "isrc", "", full.ISRC)
		fillable.Fix.Sources = []string{sourceDeezer, sourceMusicBrainz}
		return isrcResolution{findings: []finding{fillable}}, nil
	}
	return isrcResolution{findings: []finding{fillable}, reason: reason}, nil
}

func musicBrainzListsISRC(ctx context.Context, cat catalog, lib libraryTrack, candidate catalogTrack, isrc string) (bool, string, error) {
	recordings, ok := cat.(recordingCatalog)
	if !ok {
		return false, "musicbrainz was not consulted", nil
	}
	found, err := recordings.SearchRecordings(ctx, lib.Artist, lib.Title)
	if err != nil {
		return false, "musicbrainz lookup failed: " + err.Error(), blockingOnly(err)
	}
	for _, match := range sameSongRecordings(lib, found) {
		if match.hasISRC(isrc) && lengthsAgree(candidate, match.asTrack()) {
			return true, "", nil
		}
	}
	return false, "musicbrainz has no same-song recording of the deezer length listing " + isrc, nil
}

func sameSongRecordings(lib libraryTrack, found []recording) []recording {
	var same []recording
	for _, candidate := range found {
		if isSameSong(lib, candidate.asTrack()) {
			same = append(same, candidate)
		}
	}
	return same
}

func lengthsAgree(a, b catalogTrack) bool {
	return a.DurationSeconds > 0 && b.DurationSeconds > 0 && math.Abs(a.DurationSeconds-b.DurationSeconds) <= lengthToleranceSeconds
}

func confirmUnknownISRC(ctx context.Context, cat catalog, lib libraryTrack) trackResult {
	unknown := about(lib, categoryUnknownISRC, lib.ISRC)
	recordings, ok := cat.(recordingCatalog)
	if !ok {
		return trackResult{Findings: []finding{unknown}}
	}
	found, err := recordings.RecordingsByISRC(ctx, lib.ISRC)
	if err != nil {
		if blockErr := blockingOnly(err); blockErr != nil {
			return lookupFailure(ctx, lib, blockErr)
		}
		unknown.Detail += "; musicbrainz lookup failed: " + err.Error()
		return trackResult{Findings: []finding{unknown}}
	}
	if matches := sameSongRecordings(lib, found); len(matches) > 0 {
		return trackResult{Findings: []finding{about(lib, categoryISRCConfirmed, "musicbrainz recording "+matches[0].ID)}}
	}
	unknown.Detail += "; musicbrainz has no same-song recording with this ISRC"
	return trackResult{Findings: []finding{unknown}}
}

func blockingOnly(err error) error {
	if errors.Is(err, errCatalogBlocked) || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func fittingCandidates(lib libraryTrack, candidates []catalogTrack) []catalogTrack {
	var fitting []catalogTrack
	for _, candidate := range candidates {
		if isSameSong(lib, candidate) && sharesLibraryAlbum(lib, candidate) {
			fitting = append(fitting, candidate)
		}
	}
	return fitting
}

func ambiguousDetail(fitting []catalogTrack) string {
	ids := make([]string, 0, len(fitting))
	for _, candidate := range fitting {
		ids = append(ids, strconv.FormatInt(candidate.ID, 10))
	}
	return "deezer track ids " + strings.Join(ids, ", ")
}
