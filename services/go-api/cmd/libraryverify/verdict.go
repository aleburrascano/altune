package main

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"slices"
	"sort"
)

type verdict string

const (
	verdictConfirmed    verdict = "confirmed"
	verdictMismatch     verdict = "mismatch"
	verdictUnknown      verdict = "unknown"
	verdictUnofficial   verdict = "unofficial"
	verdictUnbound      verdict = "unbound"
	verdictUnverifiable verdict = "unverifiable"
	verdictFailed       verdict = "failed"
)

type identityStatus string

const (
	statusNone       identityStatus = "none"
	statusBound      identityStatus = "bound"
	statusUnofficial identityStatus = "unofficial"
	statusUnbound    identityStatus = "unbound"
)

const (
	basisRecordingID = "mb_recording_id in an AcoustID result"
	basisMergedID    = "merged mb_recording_id in an AcoustID result"
	basisISRC        = "ISRC shared with an AcoustID recording"
)

type boundIdentity struct {
	Status        identityStatus `json:"status"`
	ISRC          string         `json:"isrc,omitempty"`
	MBRecordingID string         `json:"mb_recording_id,omitempty"`
	DeezerTrackID string         `json:"deezer_track_id,omitempty"`
	AppleSongID   string         `json:"apple_song_id,omitempty"`
	Reason        string         `json:"reason,omitempty"`
}

type candidate struct {
	AcoustID        string   `json:"acoustid_id"`
	MBRecordingID   string   `json:"mb_recording_id"`
	Title           string   `json:"title,omitempty"`
	Artists         []string `json:"artists,omitempty"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	Score           float64  `json:"score"`
}

type assessment struct {
	Verdict    verdict
	Basis      string
	Candidates []candidate
}

func confidentCandidates(results []ports.AcoustIDResult, minScore float64) []candidate {
	var candidates []candidate
	for _, result := range results {
		if result.Score < minScore {
			continue
		}
		for _, recording := range result.Recordings {
			if recording.MBID == "" {
				continue
			}
			candidates = append(candidates, candidate{
				AcoustID: result.ID, MBRecordingID: recording.MBID, Title: recording.Title,
				Artists: recording.Artists, DurationSeconds: recording.Duration, Score: result.Score,
			})
		}
	}
	sort.SliceStable(candidates, func(a, b int) bool { return candidates[a].Score > candidates[b].Score })
	return candidates
}

func (a assessment) topScore() float64 {
	if len(a.Candidates) == 0 {
		return 0
	}
	return a.Candidates[0].Score
}

func hasRecording(candidates []candidate, id string) bool {
	return id != "" && slices.ContainsFunc(candidates, func(c candidate) bool { return c.MBRecordingID == id })
}

func sharesRecording(candidates []candidate, ids []string) bool {
	return slices.ContainsFunc(ids, func(id string) bool { return hasRecording(candidates, id) })
}

func judge(ctx context.Context, identity boundIdentity, candidates []candidate, lookups recordingLookups) (assessment, error) {
	switch identity.Status {
	case statusUnofficial:
		return assessment{Verdict: verdictUnofficial, Candidates: candidates}, nil
	case statusBound:
		return judgeBound(ctx, identity, candidates, lookups)
	default:
		return assessment{Verdict: verdictUnbound, Candidates: candidates}, nil
	}
}

func judgeBound(ctx context.Context, identity boundIdentity, candidates []candidate, lookups recordingLookups) (assessment, error) {
	switch {
	case identity.MBRecordingID == "" && identity.ISRC == "":
		return assessment{Verdict: verdictUnverifiable, Candidates: candidates}, nil
	case len(candidates) == 0:
		return assessment{Verdict: verdictUnknown}, nil
	case hasRecording(candidates, identity.MBRecordingID):
		return assessment{Verdict: verdictConfirmed, Basis: basisRecordingID, Candidates: candidates}, nil
	}
	basis, err := confirmThroughMusicBrainz(ctx, identity, candidates, lookups)
	switch {
	case err != nil:
		return assessment{}, err
	case basis != "":
		return assessment{Verdict: verdictConfirmed, Basis: basis, Candidates: candidates}, nil
	}
	return assessment{Verdict: verdictMismatch, Candidates: candidates}, nil
}

func confirmThroughMusicBrainz(ctx context.Context, identity boundIdentity, candidates []candidate, lookups recordingLookups) (string, error) {
	if identity.MBRecordingID != "" {
		canonical, err := lookups.CanonicalRecording(ctx, identity.MBRecordingID)
		if err != nil {
			return "", fmt.Errorf("resolve bound recording: %w", err)
		}
		if hasRecording(candidates, canonical) {
			return basisMergedID, nil
		}
	}
	if identity.ISRC == "" {
		return "", nil
	}
	recordings, err := lookups.RecordingsByISRC(ctx, identity.ISRC)
	if err != nil {
		return "", fmt.Errorf("recordings for bound ISRC: %w", err)
	}
	if sharesRecording(candidates, recordings) {
		return basisISRC, nil
	}
	return "", nil
}
