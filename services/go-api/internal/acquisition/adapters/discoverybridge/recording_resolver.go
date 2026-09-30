package discoverybridge

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	acqports "altune/go-api/internal/acquisition/ports"
	discoverydomain "altune/go-api/internal/discovery/domain"
	discoveryports "altune/go-api/internal/discovery/ports"
	discoveryservice "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/textnorm"
)

const (
	resolveLimit          = 10
	maxReferenceMBIDs     = 5
	durationToleranceSecs = 5.0
	durationTolerancePct  = 0.03
)

var _ acqports.RecordingResolver = (*RecordingResolver)(nil)

type recordingSearcher interface {
	Execute(ctx context.Context, userId shared.UserId, query *discoverydomain.SearchQuery, saveHistory bool) (*discoveryservice.SearchOutput, error)
}

type isrcAuthority interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]discoveryports.ISRCRecording, error)
}

type RecordingResolver struct {
	search recordingSearcher
	isrc   isrcAuthority
}

func NewRecordingResolver(search recordingSearcher, opts ...func(*RecordingResolver)) *RecordingResolver {
	r := &RecordingResolver{search: search}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

func WithISRCAuthority(a isrcAuthority) func(*RecordingResolver) {
	return func(r *RecordingResolver) { r.isrc = a }
}

func (r *RecordingResolver) Resolve(ctx context.Context, q acqports.RecordingQuery) (acqports.RecordingIdentity, error) {
	identity, err := r.resolveFromSearch(ctx, q)
	anchored := r.anchorToISRC(ctx, q, identity)
	if err != nil && anchored.MBID == "" {
		return acqports.RecordingIdentity{}, err
	}
	return anchored, nil
}

func (r *RecordingResolver) anchorToISRC(ctx context.Context, q acqports.RecordingQuery, identity acqports.RecordingIdentity) acqports.RecordingIdentity {
	identity.ReferenceDoubted = durationDisagrees(q.Duration, identity.Duration)
	recordings := r.isrcRecordings(ctx, q.ISRC)
	if len(recordings) == 0 {
		return withReferenceSet(identity, identity.MBID, nil)
	}
	if isrcRegistersMBID(recordings, identity.MBID) {
		return withReferenceSet(identity, identity.MBID, recordings)
	}
	return r.anchorToMismatchedISRC(ctx, q, identity, recordings)
}

func (r *RecordingResolver) anchorToMismatchedISRC(ctx context.Context, q acqports.RecordingQuery, identity acqports.RecordingIdentity, recordings []discoveryports.ISRCRecording) acqports.RecordingIdentity {
	searchMBIDUnregistered := identity.MBID != ""
	identity.ReferenceDoubted = identity.ReferenceDoubted || searchMBIDUnregistered

	chosen, ok := closestRecording(recordings, identity.Duration)
	if !ok {
		slog.InfoContext(ctx, "acquisition.isrc_anchor_ambiguous",
			"isrc", q.ISRC, "search_mbid", identity.MBID, "isrc_recordings", len(recordings))
		return withReferenceSet(identity, identity.MBID, recordings)
	}
	logAnchoredToISRC(ctx, q.ISRC, identity.MBID, chosen.MBID, searchMBIDUnregistered)
	anchored := adoptRecording(identity, q.ISRC, chosen)
	return withReferenceSet(anchored, anchored.MBID, recordings)
}

func isrcRegistersMBID(recordings []discoveryports.ISRCRecording, mbid string) bool {
	for _, rec := range recordings {
		if rec.MBID == mbid {
			return true
		}
	}
	return false
}

func logAnchoredToISRC(ctx context.Context, isrc, searchMBID, isrcMBID string, searchMBIDUnregistered bool) {
	slog.InfoContext(ctx, "acquisition.identity_anchored_to_isrc",
		"isrc", isrc, "search_mbid", searchMBID, "isrc_mbid", isrcMBID)
	if searchMBIDUnregistered {
		slog.InfoContext(ctx, "acquisition.reference_doubted",
			"search_mbid", searchMBID, "anchored_mbid", isrcMBID, "isrc", isrc)
	}
}

func withReferenceSet(identity acqports.RecordingIdentity, mbid string, recordings []discoveryports.ISRCRecording) acqports.RecordingIdentity {
	identity.MBIDs = referenceSet(mbid, recordings)
	return identity
}

func durationDisagrees(want, got float64) bool {
	if want <= 0 || got <= 0 {
		return false
	}
	tolerance := math.Max(durationToleranceSecs, want*durationTolerancePct)
	return math.Abs(got-want) > tolerance
}

func referenceSet(mbid string, recordings []discoveryports.ISRCRecording) []string {
	seen := make(map[string]bool, len(recordings)+1)
	var set []string
	add := func(id string) {
		if id == "" || seen[id] || len(set) >= maxReferenceMBIDs {
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

func (r *RecordingResolver) isrcRecordings(ctx context.Context, isrc string) []discoveryports.ISRCRecording {
	if r.isrc == nil || isrc == "" {
		return nil
	}
	recordings, err := r.isrc.RecordingsByISRC(ctx, isrc)
	if err != nil {
		slog.WarnContext(ctx, "acquisition.isrc_lookup_failed", "isrc", isrc, "error", err)
		return nil
	}
	return recordings
}

func adoptRecording(identity acqports.RecordingIdentity, isrc string, chosen discoveryports.ISRCRecording) acqports.RecordingIdentity {
	identity.MBID = chosen.MBID
	if identity.ISRC == "" {
		identity.ISRC = isrc
	}
	if chosen.Duration > 0 {
		identity.Duration = float64(chosen.Duration)
	}
	return identity
}

func closestRecording(recordings []discoveryports.ISRCRecording, want float64) (discoveryports.ISRCRecording, bool) {
	if len(recordings) == 1 {
		return recordings[0], true
	}
	if want <= 0 {
		return discoveryports.ISRCRecording{}, false
	}
	var best discoveryports.ISRCRecording
	bestGap := math.Inf(1)
	for _, rec := range recordings {
		if rec.Duration <= 0 {
			continue
		}
		if gap := math.Abs(float64(rec.Duration) - want); gap < bestGap {
			best, bestGap = rec, gap
		}
	}
	return best, best.MBID != ""
}

func (r *RecordingResolver) resolveFromSearch(ctx context.Context, q acqports.RecordingQuery) (acqports.RecordingIdentity, error) {
	if r.search == nil || q.Title == "" || q.Artist == "" {
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

func (r *RecordingResolver) searchTracks(ctx context.Context, q acqports.RecordingQuery) ([]discoverydomain.SearchResult, error) {
	query, err := discoverydomain.NewSearchQuery(
		q.Artist+" "+q.Title,
		map[discoverydomain.ResultKind]bool{discoverydomain.ResultKindTrack: true},
		resolveLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("build resolve query: %w", err)
	}
	out, err := r.search.Execute(ctx, shared.UserId{}, query, false)
	if err != nil {
		return nil, fmt.Errorf("resolve recording: %w", err)
	}
	if out == nil {
		return nil, nil
	}
	return out.Results, nil
}

func pickRecording(results []discoverydomain.SearchResult, q acqports.RecordingQuery) (discoverydomain.SearchResult, bool) {
	if q.ISRC != "" {
		for _, res := range results {
			if strings.EqualFold(res.ISRC, q.ISRC) {
				return res, true
			}
		}
	}

	wantTitle := textnorm.NormalizeForMatch(q.Title)
	wantArtist := textnorm.NormalizeForMatch(q.Artist)
	for _, res := range results {
		if res.Kind != discoverydomain.ResultKindTrack {
			continue
		}
		if textnorm.NormalizeForMatch(res.Title) != wantTitle {
			continue
		}
		if wantArtist != "" && textnorm.NormalizeForMatch(res.Subtitle) != wantArtist {
			continue
		}
		return res, true
	}
	return discoverydomain.SearchResult{}, false
}

func toIdentity(res discoverydomain.SearchResult) acqports.RecordingIdentity {
	identity := acqports.RecordingIdentity{
		ISRC:     res.ISRC,
		MBID:     res.MBID,
		Duration: float64(res.Duration),
	}
	for _, src := range res.Sources {
		if src.ExternalID == "" {
			continue
		}
		identity.Sources = append(identity.Sources, acqports.ProviderRef{
			Provider:   providerKey(src.Provider),
			ExternalID: src.ExternalID,
			URL:        src.URL,
		})
	}
	return identity
}

func providerKey(p discoverydomain.ProviderName) acqports.RecordingProvider {
	switch p {
	case discoverydomain.ProviderYouTube:
		return acqports.ProviderYouTube
	case discoverydomain.ProviderDeezer:
		return acqports.ProviderDeezer
	case discoverydomain.ProviderSoundCloud:
		return acqports.ProviderSoundCloud
	default:
		return acqports.RecordingProvider(p.String())
	}
}
