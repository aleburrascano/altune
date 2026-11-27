package musicbrainzmatch

import (
	"altune/go-api/internal/download/internal/core/tracklength"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/titlemarkers"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"

	acqports "altune/go-api/internal/download/internal/core/ports"
)

const resolveLimit = 10

var (
	_ acqports.RecordingResolver = (*Resolver)(nil)
	_ acqports.RecordingLookup   = (*Resolver)(nil)
)

var ErrMissingPort = errors.New("musicbrainzmatch: a required port is missing")

type Ports struct {
	Search      acqports.RecordingSearch
	ISRC        acqports.ISRCLookup
	Recording   acqports.RecordingLookup
	Rerecording acqports.RerecordingLookup
}

type Resolver struct {
	search      acqports.RecordingSearch
	isrc        acqports.ISRCLookup
	lookup      acqports.RecordingLookup
	rerecording acqports.RerecordingLookup
}

func New(p Ports) (*Resolver, error) {
	missing := []struct {
		name string
		gone bool
	}{
		{"Search", p.Search == nil},
		{"ISRC", p.ISRC == nil},
		{"Recording", p.Recording == nil},
		{"Rerecording", p.Rerecording == nil},
	}
	for _, port := range missing {
		if port.gone {
			return nil, fmt.Errorf("%w: Ports.%s", ErrMissingPort, port.name)
		}
	}
	return &Resolver{search: p.Search, isrc: p.ISRC, lookup: p.Recording, rerecording: p.Rerecording}, nil
}

func MustNew(p Ports) *Resolver {
	r, err := New(p)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *Resolver) RecordingByMBID(ctx context.Context, mbid string) (acqports.RecordingFacts, bool, error) {
	return r.lookup.RecordingByMBID(ctx, mbid)
}

func (r *Resolver) Resolve(ctx context.Context, q acqports.RecordingQuery) (acqports.RecordingIdentity, error) {
	if q.MBID != "" {
		return r.resolvePinned(ctx, q), nil
	}
	identity, err := r.resolveFromSearch(ctx, q)
	anchored := r.anchorToISRC(ctx, q, identity)
	if err != nil && anchored.MBID == "" {
		return acqports.RecordingIdentity{}, err
	}
	return anchored, nil
}

func (r *Resolver) resolvePinned(ctx context.Context, q acqports.RecordingQuery) acqports.RecordingIdentity {
	identity := r.pinnedIdentity(ctx, q.MBID)
	searched, err := r.resolveFromSearch(ctx, q)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.pin_search_failed", "pinned_mbid", q.MBID, "error", err)
	}
	if searched.MBID != "" && searched.MBID != q.MBID {
		slog.InfoContext(ctx, "acquisition.identity_pin_disagrees",
			"pinned_mbid", q.MBID, "search_mbid", searched.MBID)
		return identity
	}
	if searched.MBID == q.MBID {
		identity.Sources = searched.Sources
	}
	return identity
}

func (r *Resolver) pinnedIdentity(ctx context.Context, pin string) acqports.RecordingIdentity {
	identity := acqports.RecordingIdentity{MBID: pin, MBIDs: []string{pin}}
	facts, found := r.pinnedFacts(ctx, pin)
	if !found {
		return identity
	}
	identity.Duration = facts.Duration
	if len(facts.ISRCs) == 1 {
		identity.ISRC = facts.ISRCs[0]
		identity.MBIDs = referenceSet(pin, r.isrcRecordings(ctx, identity.ISRC))
	}
	return identity
}

func (r *Resolver) pinnedFacts(ctx context.Context, pin string) (acqports.RecordingFacts, bool) {
	facts, found, err := r.RecordingByMBID(ctx, pin)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.pinned_recording_lookup_failed", "pinned_mbid", pin, "error", err)
		return acqports.RecordingFacts{}, false
	}
	if !found {
		slog.InfoContext(ctx, "acquisition.pinned_recording_unknown", "pinned_mbid", pin)
	}
	return facts, found
}

func (r *Resolver) anchorToISRC(ctx context.Context, q acqports.RecordingQuery, identity acqports.RecordingIdentity) acqports.RecordingIdentity {
	lengthDisagrees := durationDisagrees(q.Duration, identity.Duration)
	identity.ReferenceDoubted = lengthDisagrees
	recordings := r.isrcRecordings(ctx, q.ISRC)
	if len(recordings) == 0 {
		return withReferenceSet(identity, identity.MBID, nil)
	}
	if isrcRegistersMBID(recordings, identity.MBID) {
		identity.ReferenceDoubted = false
		if lengthDisagrees {
			slog.InfoContext(ctx, "acquisition.catalog_length_conflict",
				"isrc", q.ISRC, "mbid", identity.MBID,
				"catalog_seconds", q.Duration, "reference_seconds", identity.Duration)
		}
		return withReferenceSet(identity, identity.MBID, recordings)
	}
	return r.anchorToMismatchedISRC(ctx, q, identity, recordings)
}

func (r *Resolver) anchorToMismatchedISRC(ctx context.Context, q acqports.RecordingQuery, identity acqports.RecordingIdentity, recordings []acqports.ISRCRecording) acqports.RecordingIdentity {
	identity.ReferenceDoubted = identity.ReferenceDoubted || identity.MBID != ""

	chosen, ok := closestRecording(recordings, expectedLength(q.Duration, identity.Duration))
	if !ok {
		slog.InfoContext(ctx, "acquisition.isrc_anchor_ambiguous",
			"isrc", q.ISRC, "search_mbid", identity.MBID, "isrc_recordings", len(recordings))
		return withReferenceSet(identity, identity.MBID, recordings)
	}
	logAnchoredToISRC(ctx, q.ISRC, identity.MBID, chosen.MBID)
	anchored := adoptRecording(identity, q.ISRC, chosen)
	return withReferenceSet(anchored, anchored.MBID, recordings)
}

func isrcRegistersMBID(recordings []acqports.ISRCRecording, mbid string) bool {
	for _, rec := range recordings {
		if rec.MBID == mbid {
			return true
		}
	}
	return false
}

func logAnchoredToISRC(ctx context.Context, isrc, searchMBID, isrcMBID string) {
	slog.InfoContext(ctx, "acquisition.identity_anchored_to_isrc",
		"isrc", isrc, "search_mbid", searchMBID, "isrc_mbid", isrcMBID)
	if searchMBID != "" {
		slog.InfoContext(ctx, "acquisition.reference_doubted",
			"search_mbid", searchMBID, "anchored_mbid", isrcMBID, "isrc", isrc)
	}
}

func withReferenceSet(identity acqports.RecordingIdentity, mbid string, recordings []acqports.ISRCRecording) acqports.RecordingIdentity {
	identity.MBIDs = referenceSet(mbid, recordings)
	return identity
}

func expectedLength(queried, searched float64) float64 {
	if queried > 0 {
		return queried
	}
	return searched
}

func durationDisagrees(want, got float64) bool {
	if want <= 0 || got <= 0 {
		return false
	}
	return !tracklength.SameRecording(want, got)
}

func referenceSet(mbid string, recordings []acqports.ISRCRecording) []string {
	seen := make(map[string]bool, len(recordings)+1)
	var set []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		set = append(set, id)
	}
	add(mbid)
	for _, rec := range recordings {
		add(rec.MBID)
	}
	return set
}

func (r *Resolver) isrcRecordings(ctx context.Context, isrc string) []acqports.ISRCRecording {
	if isrc == "" {
		return nil
	}
	recordings, err := r.isrc.RecordingsByISRC(ctx, isrc)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.isrc_lookup_failed", "isrc", isrc, "error", err)
		return nil
	}
	return recordings
}

func adoptRecording(identity acqports.RecordingIdentity, isrc string, chosen acqports.ISRCRecording) acqports.RecordingIdentity {
	identity.MBID = chosen.MBID
	if identity.ISRC == "" {
		identity.ISRC = isrc
	}
	if chosen.Duration > 0 {
		identity.Duration = chosen.Duration
	}
	return identity
}

func closestRecording(recordings []acqports.ISRCRecording, want float64) (acqports.ISRCRecording, bool) {
	if len(recordings) == 1 {
		return recordings[0], true
	}
	if want <= 0 {
		return acqports.ISRCRecording{}, false
	}
	var best acqports.ISRCRecording
	bestGap := math.Inf(1)
	for _, rec := range recordings {
		if rec.Duration <= 0 {
			continue
		}
		if gap := math.Abs(rec.Duration - want); gap < bestGap {
			best, bestGap = rec, gap
		}
	}
	return best, best.MBID != ""
}

func (r *Resolver) resolveFromSearch(ctx context.Context, q acqports.RecordingQuery) (acqports.RecordingIdentity, error) {
	if q.Title == "" || q.Artist == "" {
		return acqports.RecordingIdentity{}, nil
	}
	results, err := r.searchTracks(ctx, q)
	if err != nil || len(results) == 0 {
		return acqports.RecordingIdentity{}, err
	}
	best, ok := pickRecording(results, q)
	if !ok {
		return acqports.RecordingIdentity{}, nil
	}
	return toIdentity(best), nil
}

func (r *Resolver) searchTracks(ctx context.Context, q acqports.RecordingQuery) ([]acqports.SearchedRecording, error) {
	results, err := r.search.SearchRecordings(ctx, q.Artist+" "+q.Title, resolveLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve recording: %w", err)
	}
	return results, nil
}

func pickByISRC(results []acqports.SearchedRecording, isrc string) (acqports.SearchedRecording, bool) {
	if isrc == "" {
		return acqports.SearchedRecording{}, false
	}
	for _, res := range results {
		if strings.EqualFold(res.ISRC, isrc) {
			return res, true
		}
	}
	return acqports.SearchedRecording{}, false
}

func pickRecording(results []acqports.SearchedRecording, q acqports.RecordingQuery) (acqports.SearchedRecording, bool) {
	if res, ok := pickByISRC(results, q.ISRC); ok {
		return res, true
	}
	wantTitle := textnorm.NormalizeForMatch(q.Title)
	wantArtist := textnorm.NormalizeForMatch(q.Artist)
	wantLabels := titlemarkers.RecordingLabels(q.Title)
	for _, res := range results {
		if textnorm.NormalizeForMatch(res.Title) != wantTitle || !slices.Equal(titlemarkers.RecordingLabels(res.Title), wantLabels) {
			continue
		}
		if wantArtist != "" && textnorm.NormalizeForMatch(res.Artist) != wantArtist {
			continue
		}
		return res, true
	}
	return acqports.SearchedRecording{}, false
}

func toIdentity(res acqports.SearchedRecording) acqports.RecordingIdentity {
	identity := acqports.RecordingIdentity{
		ISRC:     res.ISRC,
		MBID:     res.MBID,
		Duration: res.Duration,
	}
	for _, src := range res.Sources {
		if src.ExternalID != "" {
			identity.Sources = append(identity.Sources, src)
		}
	}
	return identity
}
