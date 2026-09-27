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

const resolveLimit = 10

var _ acqports.RecordingResolver = (*RecordingResolver)(nil)

type recordingSearcher interface {
	Execute(ctx context.Context, userId shared.UserId, query *discoverydomain.SearchQuery, saveHistory bool) (*discoveryservice.SearchOutput, error)
}

// isrcAuthority answers which recordings an ISRC is registered against. It is
// the MusicBrainz ISRC lookup in production.
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

// WithISRCAuthority makes the ISRC's own recordings outrank the MBID a search
// result carries. A merged search result can pair a track's ISRC with the MBID
// of a same-titled remix, and the fingerprint check then rejects every correct
// upload against the remix's AcoustID cluster.
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

// anchorToISRC swaps the identity's MBID for one of the ISRC's recordings when
// the search picked an MBID the ISRC is not registered against. The duration
// follows the MBID, because a mismatched MBID means the merged result's length
// may belong to the wrong recording too. Without an ISRC, an authority, or an
// answer from it, the identity passes through untouched.
func (r *RecordingResolver) anchorToISRC(ctx context.Context, q acqports.RecordingQuery, identity acqports.RecordingIdentity) acqports.RecordingIdentity {
	recordings := r.isrcRecordings(ctx, q.ISRC)
	if len(recordings) == 0 {
		return identity
	}
	for _, rec := range recordings {
		if rec.MBID == identity.MBID {
			return identity
		}
	}

	chosen := closestRecording(recordings, identity.Duration)
	slog.InfoContext(ctx, "acquisition.identity_anchored_to_isrc",
		"isrc", q.ISRC, "search_mbid", identity.MBID, "isrc_mbid", chosen.MBID)
	return adoptRecording(identity, q.ISRC, chosen)
}

// isrcRecordings asks the authority for the ISRC's recordings. No authority, no
// ISRC, or a failed lookup all answer nothing, so the caller keeps its identity.
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

// closestRecording picks the ISRC recording nearest the search's length, or the
// first one when there is no length to compare against.
func closestRecording(recordings []discoveryports.ISRCRecording, want float64) discoveryports.ISRCRecording {
	best := recordings[0]
	if want <= 0 {
		return best
	}
	bestGap := math.Inf(1)
	for _, rec := range recordings {
		if rec.Duration <= 0 {
			continue
		}
		if gap := math.Abs(float64(rec.Duration) - want); gap < bestGap {
			best, bestGap = rec, gap
		}
	}
	return best
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
		identity.Sources = append(identity.Sources, acqports.RecordingSource{
			Provider:   providerKey(src.Provider),
			ExternalID: src.ExternalID,
			URL:        src.URL,
		})
	}
	return identity
}

// providerKey maps a discovery provider onto the acquisition identity key that
// source adapters look up, so both sides share acqports' constants. Providers no
// adapter consumes keep their discovery string form.
func providerKey(p discoverydomain.ProviderName) string {
	switch p {
	case discoverydomain.ProviderYouTube:
		return acqports.ProviderYouTube
	case discoverydomain.ProviderDeezer:
		return acqports.ProviderDeezer
	case discoverydomain.ProviderSoundCloud:
		return acqports.ProviderSoundCloud
	default:
		return p.String()
	}
}
