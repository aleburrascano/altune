package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/music"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"slices"
	"strings"
)

const resolveLimit = 10

var (
	_ ports.RecordingResolver = (*MusicBrainzResolver)(nil)
	_ ports.RecordingLookup   = (*MusicBrainzResolver)(nil)
)

var ErrMissingResolverPort = errors.New("musicbrainzmatch: a required port is missing")

type ResolverPorts struct {
	Search      ports.RecordingSearch
	ISRC        ports.ISRCLookup
	Recording   ports.RecordingLookup
	Rerecording ports.RerecordingLookup
}

type MusicBrainzResolver struct {
	search      ports.RecordingSearch
	isrc        ports.ISRCLookup
	lookup      ports.RecordingLookup
	rerecording ports.RerecordingLookup
}

func NewMusicBrainzResolver(p ResolverPorts) (*MusicBrainzResolver, error) {
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
			return nil, fmt.Errorf("%w: Ports.%s", ErrMissingResolverPort, port.name)
		}
	}
	return &MusicBrainzResolver{search: p.Search, isrc: p.ISRC, lookup: p.Recording, rerecording: p.Rerecording}, nil
}

func MustNewMusicBrainzResolver(p ResolverPorts) *MusicBrainzResolver {
	r, err := NewMusicBrainzResolver(p)
	if err != nil {
		panic(err)
	}
	return r
}

func (r *MusicBrainzResolver) RecordingByMBID(ctx context.Context, mbid string) (domain.RecordingFacts, bool, error) {
	return r.lookup.RecordingByMBID(ctx, mbid)
}

func (r *MusicBrainzResolver) Resolve(ctx context.Context, q music.RecordingIdentity) (domain.RecordingIdentity, error) {
	if q.RecordingMBID != "" {
		return r.resolvePinned(ctx, q), nil
	}
	identity, err := r.resolveFromSearch(ctx, q)
	anchored := r.anchorToISRC(ctx, q, identity)
	if err != nil && anchored.MBID == "" {
		return domain.RecordingIdentity{}, err
	}
	return anchored, nil
}

func (r *MusicBrainzResolver) resolvePinned(ctx context.Context, q music.RecordingIdentity) domain.RecordingIdentity {
	identity := r.pinnedIdentity(ctx, q.RecordingMBID)
	searched, err := r.resolveFromSearch(ctx, q)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.pin_search_failed", "pinned_mbid", q.RecordingMBID, logging.KeyError, err)
	}
	if searched.MBID != "" && searched.MBID != q.RecordingMBID {
		slog.InfoContext(ctx, "acquisition.identity_pin_disagrees",
			"pinned_mbid", q.RecordingMBID, "search_mbid", searched.MBID)
		return identity
	}
	if searched.MBID == q.RecordingMBID {
		identity.Sources = searched.Sources
	}
	return identity
}

func (r *MusicBrainzResolver) pinnedIdentity(ctx context.Context, pin string) domain.RecordingIdentity {
	identity := domain.RecordingIdentity{MBID: pin, MBIDs: []string{pin}}
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

func (r *MusicBrainzResolver) pinnedFacts(ctx context.Context, pin string) (domain.RecordingFacts, bool) {
	facts, found, err := r.RecordingByMBID(ctx, pin)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.pinned_recording_lookup_failed", "pinned_mbid", pin, logging.KeyError, err)
		return domain.RecordingFacts{}, false
	}
	if !found {
		slog.InfoContext(ctx, "acquisition.pinned_recording_unknown", "pinned_mbid", pin)
	}
	return facts, found
}

func (r *MusicBrainzResolver) anchorToISRC(ctx context.Context, q music.RecordingIdentity, identity domain.RecordingIdentity) domain.RecordingIdentity {
	lengthDisagrees := durationDisagrees(q.DurationSeconds, identity.Duration)
	identity.ReferenceDoubted = lengthDisagrees
	recordings := r.isrcRecordings(ctx, q.ISRC)
	if len(recordings) == 0 {
		return withReferenceSet(identity, identity.MBID, nil)
	}
	if !isrcRegistersMBID(recordings, identity.MBID) {
		return r.anchorToMismatchedISRC(ctx, q, identity, recordings)
	}
	identity.ReferenceDoubted = false
	if lengthDisagrees {
		slog.InfoContext(ctx, "acquisition.catalog_length_conflict",
			"isrc", q.ISRC, "mbid", identity.MBID,
			"catalog_seconds", q.DurationSeconds, "reference_seconds", identity.Duration)
	}
	return withReferenceSet(identity, identity.MBID, recordings)
}

func (r *MusicBrainzResolver) anchorToMismatchedISRC(ctx context.Context, q music.RecordingIdentity, identity domain.RecordingIdentity, recordings []domain.ISRCRecording) domain.RecordingIdentity {
	identity.ReferenceDoubted = identity.ReferenceDoubted || identity.MBID != ""

	chosen, ok := closestRecording(recordings, expectedLength(q.DurationSeconds, identity.Duration))
	if !ok {
		slog.InfoContext(ctx, "acquisition.isrc_anchor_ambiguous",
			"isrc", q.ISRC, "search_mbid", identity.MBID, "isrc_recordings", len(recordings))
		return withReferenceSet(identity, identity.MBID, recordings)
	}
	logAnchoredToISRC(ctx, q.ISRC, identity.MBID, chosen.MBID)
	anchored := adoptRecording(identity, q.ISRC, chosen)
	return withReferenceSet(anchored, anchored.MBID, recordings)
}

func isrcRegistersMBID(recordings []domain.ISRCRecording, mbid string) bool {
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

func withReferenceSet(identity domain.RecordingIdentity, mbid string, recordings []domain.ISRCRecording) domain.RecordingIdentity {
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
	return !SameRecording(want, got)
}

func referenceSet(mbid string, recordings []domain.ISRCRecording) []string {
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

func (r *MusicBrainzResolver) isrcRecordings(ctx context.Context, isrc string) []domain.ISRCRecording {
	if isrc == "" {
		return nil
	}
	recordings, err := r.isrc.RecordingsByISRC(ctx, isrc)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.isrc_lookup_failed", "isrc", isrc, logging.KeyError, err)
		return nil
	}
	return recordings
}

func adoptRecording(identity domain.RecordingIdentity, isrc string, chosen domain.ISRCRecording) domain.RecordingIdentity {
	identity.MBID = chosen.MBID
	if identity.ISRC == "" {
		identity.ISRC = isrc
	}
	if chosen.Duration > 0 {
		identity.Duration = chosen.Duration
	}
	return identity
}

func closestRecording(recordings []domain.ISRCRecording, want float64) (domain.ISRCRecording, bool) {
	if len(recordings) == 1 {
		return recordings[0], true
	}
	if want <= 0 {
		return domain.ISRCRecording{}, false
	}
	var best domain.ISRCRecording
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

func (r *MusicBrainzResolver) resolveFromSearch(ctx context.Context, q music.RecordingIdentity) (domain.RecordingIdentity, error) {
	if q.Title == "" || q.Artist == "" {
		return domain.RecordingIdentity{}, nil
	}
	results, err := r.searchTracks(ctx, q)
	if err != nil || len(results) == 0 {
		return domain.RecordingIdentity{}, err
	}
	best, ok := pickRecording(results, q)
	if !ok {
		return domain.RecordingIdentity{}, nil
	}
	return toIdentity(best), nil
}

func (r *MusicBrainzResolver) searchTracks(ctx context.Context, q music.RecordingIdentity) ([]music.RecordingIdentity, error) {
	results, err := r.search.SearchRecordings(ctx, q.Artist+" "+q.Title, resolveLimit)
	if err != nil {
		return nil, fmt.Errorf("resolve recording: %w", err)
	}
	return results, nil
}

func pickByISRC(results []music.RecordingIdentity, isrc string) (music.RecordingIdentity, bool) {
	if isrc == "" {
		return music.RecordingIdentity{}, false
	}
	for _, res := range results {
		if strings.EqualFold(res.ISRC, isrc) {
			return res, true
		}
	}
	return music.RecordingIdentity{}, false
}

func pickRecording(results []music.RecordingIdentity, q music.RecordingIdentity) (music.RecordingIdentity, bool) {
	if res, ok := pickByISRC(results, q.ISRC); ok {
		return res, true
	}
	wantTitle := music.NormalizeForMatch(q.Title)
	wantArtist := music.NormalizeForMatch(q.Artist)
	wantLabels := music.RecordingLabels(q.Title)
	var best music.RecordingIdentity
	bestAgreement := -1
	for _, res := range results {
		if music.NormalizeForMatch(res.Title) != wantTitle || !slices.Equal(music.RecordingLabels(res.Title), wantLabels) {
			continue
		}
		if wantArtist != "" && music.NormalizeForMatch(res.Artist) != wantArtist {
			continue
		}
		if agreed := agreement(res, q); agreed > bestAgreement {
			best, bestAgreement = res, agreed
		}
	}
	return best, bestAgreement >= 0
}

func agreement(res, q music.RecordingIdentity) int {
	score := 0
	if album := music.NormalizeForMatch(q.Album); album != "" && music.NormalizeForMatch(res.Album) == album {
		score += 4
	}
	if q.Explicit != nil && res.Explicit != nil && *q.Explicit == *res.Explicit {
		score += 2
	}
	if q.Year != 0 && res.Year == q.Year {
		score++
	}
	if q.Position.Track != 0 && res.Position.Track == q.Position.Track {
		score++
	}
	return score
}

func toIdentity(res music.RecordingIdentity) domain.RecordingIdentity {
	identity := domain.RecordingIdentity{
		ISRC:     res.ISRC,
		MBID:     res.RecordingMBID,
		Duration: res.DurationSeconds,
	}
	for _, src := range res.Sources {
		if src.ExternalID != "" {
			identity.Sources = append(identity.Sources, domain.ProviderRef{
				Provider: domain.RecordingProvider(src.Provider), ExternalID: src.ExternalID, URL: src.URL,
			})
		}
	}
	return identity
}
