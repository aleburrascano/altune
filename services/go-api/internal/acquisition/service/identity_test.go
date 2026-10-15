package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

func TestRecordingIdentity_SourceFor(t *testing.T) {
	id := ports.RecordingIdentity{Sources: []ports.ProviderRef{
		{Provider: "deezer", ExternalID: "123"},
		{Provider: "youtube", ExternalID: "abc"},
	}}

	if got, ok := id.SourceFor("youtube"); !ok || got.ExternalID != "abc" {
		t.Errorf("SourceFor(youtube) = %+v %v", got, ok)
	}
	if _, ok := id.SourceFor("tidal"); ok {
		t.Error("SourceFor(tidal) should report missing")
	}
}

type clusterErrIdentifier struct{ err error }

func (clusterErrIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	return ports.RecordingMatch{}, nil
}

func (i clusterErrIdentifier) AcoustIDsFor(context.Context, string) ([]string, error) {
	return nil, i.err
}

func TestResolveIdentity_ClusterLookupErrorLogEvent(t *testing.T) {
	cases := []struct {
		name      string
		err       error
		want      string
		notWanted string
	}{
		{
			name:      "throttled",
			err:       fmt.Errorf("acoustid lookup: %w", acqdomain.ErrIdentifyThrottled),
			want:      "acquisition.identify_throttled",
			notWanted: "acquisition.expected_cluster_failed",
		},
		{
			name:      "other failure",
			err:       errors.New("boom"),
			want:      "acquisition.expected_cluster_failed",
			notWanted: "acquisition.identify_throttled",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureJSONLog(t)
			ac := &AcquisitionContext{Track: TrackRef{ID: "t1"}}
			resolver := &stubResolver{identity: ports.RecordingIdentity{MBID: "mb-1"}}

			ResolveIdentity(context.Background(), resolver, clusterErrIdentifier{err: tc.err}, ac)

			rec := findLogRecord(t, logs, tc.want)
			if rec["track_id"] != "t1" || rec["mbid"] != "mb-1" {
				t.Errorf("log attrs = %v", rec)
			}
			if strings.Contains(logs.String(), tc.notWanted) {
				t.Errorf("unexpected %s: %s", tc.notWanted, logs.String())
			}
			if len(ac.Identity.AcoustIDs) != 0 {
				t.Errorf("AcoustIDs = %v, want empty", ac.Identity.AcoustIDs)
			}
		})
	}
}

type clusterIdentifier struct {
	clusters map[string][]string
	asked    []string
}

func (*clusterIdentifier) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	return ports.RecordingMatch{}, nil
}

func (i *clusterIdentifier) AcoustIDsFor(_ context.Context, mbid string) ([]string, error) {
	i.asked = append(i.asked, mbid)
	return i.clusters[mbid], nil
}

const (
	pinnedBoysDontCry = "2b2af12b-48f1-444e-8591-cacb9f88f02a"
	pinnedCluster     = "4881574c-d77f-458f-89e6-c9010b56266e"
)

func pinnedTrack() TrackRef {
	return TrackRef{ID: "t1", Title: "Boys Don't Cry", Artist: "The Cure", CatalogMBID: pinnedBoysDontCry}
}

func TestResolveIdentity_PinnedTrackTheResolverKnowsNothingAboutStillGetsTheFingerprintGate(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry || !slices.Equal(ac.Identity.MBIDs, []string{pinnedBoysDontCry}) {
		t.Errorf("identity = %+v, want the catalog pin as the recording and its only reference", ac.Identity)
	}
	if !slices.Equal(ac.Identity.AcoustIDs, []string{pinnedCluster}) {
		t.Errorf("AcoustIDs = %v, want the pinned recording's cluster", ac.Identity.AcoustIDs)
	}
}

func TestResolveIdentity_PinnedTrackSurvivesAResolverFailure(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{err: errors.New("musicbrainz down")}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry || !slices.Equal(ac.Identity.AcoustIDs, []string{pinnedCluster}) {
		t.Errorf("identity = %+v, want the pin and its cluster despite the failed resolve", ac.Identity)
	}
}

func TestResolveIdentity_ResolvedPinnedIdentityIsAdoptedAsIs(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	resolved := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry, "isrc-sibling"}, Duration: 157.306}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{identity: resolved}, identifier, ac)

	if !slices.Equal(ac.Identity.MBIDs, resolved.MBIDs) || ac.Identity.Duration != 157.306 || ac.Track.Duration != 157.306 {
		t.Errorf("identity = %+v, track duration %v, want the resolver's pinned identity untouched", ac.Identity, ac.Track.Duration)
	}
	if !slices.Equal(identifier.asked, []string{pinnedBoysDontCry}) {
		t.Errorf("cluster asked for %v, want only the pin", identifier.asked)
	}
}

func TestResolveIdentity_UnpinnedTrackWithNothingResolvedStaysWithoutIdentity(t *testing.T) {
	identifier := &clusterIdentifier{}
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1", Title: "Boys Don't Cry", Artist: "The Cure"}}

	ResolveIdentity(context.Background(), &stubResolver{}, identifier, ac)

	if !ac.Identity.IsZero() || len(identifier.asked) != 0 {
		t.Errorf("identity = %+v, asked %v; want no identity and no cluster lookup without a pin", ac.Identity, identifier.asked)
	}
}

func TestResolveIdentity_ResolverIsAskedForTheCatalogPin(t *testing.T) {
	resolver := &stubResolver{}

	ResolveIdentity(context.Background(), resolver, nil, &AcquisitionContext{Track: pinnedTrack()})

	if len(resolver.queries) != 1 || resolver.queries[0].MBID != pinnedBoysDontCry {
		t.Errorf("queries = %+v, want one carrying the catalog pin", resolver.queries)
	}
}
