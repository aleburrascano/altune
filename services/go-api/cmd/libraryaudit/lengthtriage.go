package main

import (
	"context"
	"errors"
	"strconv"
)

const (
	categoryLengthVersionFix category = "length_version_fix"
	categoryLengthReacquire  category = "length_reacquire"
)

type lengthAction string

const (
	actionFixISRC   lengthAction = "fix_isrc"
	actionReacquire lengthAction = "reacquire"
)

type lengthPlan struct {
	Action         lengthAction `json:"action"`
	UserID         string       `json:"user_id"`
	Version        int          `json:"version"`
	OldISRC        string       `json:"old_isrc"`
	NewISRC        string       `json:"new_isrc,omitempty"`
	FileSeconds    float64      `json:"file_seconds"`
	CatalogSeconds float64      `json:"catalog_seconds"`
}

type recordingSource interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error)
}

func isLengthFinding(f finding) bool {
	return f.Category == categoryLengthShorter || f.Category == categoryLengthLonger
}

func triageLengthFindings(ctx context.Context, cat catalog, recordings recordingSource, tracks []libraryTrack, findings []finding) ([]finding, error) {
	byID := make(map[string]libraryTrack, len(tracks))
	for _, lib := range tracks {
		byID[lib.ID] = lib
	}
	out := append([]finding(nil), findings...)
	for _, f := range findings {
		if !isLengthFinding(f) {
			continue
		}
		triaged, err := triageTrack(ctx, cat, recordings, byID[f.TrackID])
		if errors.Is(err, errCatalogBlocked) || ctx.Err() != nil {
			return out, errors.Join(err, ctx.Err())
		}
		if err != nil {
			triaged = about(byID[f.TrackID], categoryLookupFailed, "length triage: "+err.Error())
		}
		out = append(out, triaged)
	}
	return out, nil
}

func triageTrack(ctx context.Context, cat catalog, recordings recordingSource, lib libraryTrack) (finding, error) {
	reference, err := catalogRecord(ctx, cat, lib)
	if err != nil {
		return finding{}, err
	}
	versions, err := otherFittingVersions(ctx, cat, lib, reference)
	if err != nil {
		return finding{}, err
	}
	if len(versions) == 1 {
		confirmed, err := musicBrainzConfirms(ctx, recordings, lib, versions[0])
		if err != nil {
			return finding{}, err
		}
		if confirmed {
			return versionFix(lib, versions[0]), nil
		}
	}
	return reacquireFinding(lib, reference, len(versions)), nil
}

func otherFittingVersions(ctx context.Context, cat catalog, lib libraryTrack, reference catalogTrack) ([]catalogTrack, error) {
	candidates, err := cat.Search(ctx, lib.Artist, lib.Title)
	if err != nil {
		return nil, err
	}
	var versions []catalogTrack
	seen := map[string]bool{canonicalISRC(lib.ISRC): true}
	for _, candidate := range fittingCandidates(lib, candidates) {
		if candidate.ID == reference.ID {
			continue
		}
		full, err := cat.TrackByID(ctx, candidate.ID)
		if err != nil {
			return nil, err
		}
		if key := canonicalISRC(full.ISRC); key != "" && !seen[key] {
			seen[key] = true
			versions = append(versions, full)
		}
	}
	return versions, nil
}

func musicBrainzConfirms(ctx context.Context, recordings recordingSource, lib libraryTrack, version catalogTrack) (bool, error) {
	found, err := recordings.RecordingsByISRC(ctx, version.ISRC)
	if errors.Is(err, errCatalogUnknown) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, rec := range found {
		if rec.hasISRC(version.ISRC) && recordingMatches(lib, version, rec) {
			return true, nil
		}
	}
	return false, nil
}

func recordingMatches(lib libraryTrack, version catalogTrack, rec recording) bool {
	if !titlesAgree(lib.Title, rec.Title) {
		return false
	}
	return lengthsAgree(version, catalogTrack{DurationSeconds: rec.DurationSeconds}) && anyNameAgrees(lib.Artist, rec.Artists)
}

func anyNameAgrees(name string, others []string) bool {
	for _, other := range others {
		if namesAgree(name, other) {
			return true
		}
	}
	return false
}

func versionFix(lib libraryTrack, version catalogTrack) finding {
	detail := "isrc " + lib.ISRC + " -> " + version.ISRC + ": deezer and musicbrainz both give a " +
		strconv.Itoa(int(version.DurationSeconds)) + "s recording for the " + strconv.Itoa(int(lib.DurationSeconds)) + "s file"
	f := about(lib, categoryLengthVersionFix, detail)
	f.LengthPlan = &lengthPlan{
		Action: actionFixISRC, UserID: lib.UserID, Version: lib.Version,
		OldISRC: lib.ISRC, NewISRC: version.ISRC,
		FileSeconds: lib.DurationSeconds, CatalogSeconds: version.DurationSeconds,
	}
	return f
}

func reacquireFinding(lib libraryTrack, reference catalogTrack, fittingVersions int) finding {
	detail := strconv.Itoa(int(lib.DurationSeconds)) + "s audio vs " + strconv.Itoa(int(reference.DurationSeconds)) + "s catalog"
	switch fittingVersions {
	case 0:
		detail += ", no other version fits the file"
	case 1:
		detail += ", one other version fits but musicbrainz does not confirm it"
	default:
		detail += ", " + strconv.Itoa(fittingVersions) + " other versions fit the file"
	}
	f := about(lib, categoryLengthReacquire, detail)
	f.LengthPlan = &lengthPlan{
		Action: actionReacquire, UserID: lib.UserID, Version: lib.Version,
		OldISRC: lib.ISRC, FileSeconds: lib.DurationSeconds, CatalogSeconds: reference.DurationSeconds,
	}
	return f
}
