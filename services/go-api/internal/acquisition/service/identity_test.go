package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"
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

func TestResolveIdentity_SpendsNoClusterLookup(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{identity: ports.RecordingIdentity{MBID: pinnedBoysDontCry}}, identifier, ac)

	if len(identifier.asked) != 0 {
		t.Errorf("cluster asked for %v, want no AcoustID quota spent on identity resolution", identifier.asked)
	}
}

func TestResolveIdentity_PinnedTrackTheResolverKnowsNothingAboutStillGetsTheFingerprintGate(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry || !slices.Equal(ac.Identity.MBIDs, []string{pinnedBoysDontCry}) {
		t.Errorf("identity = %+v, want the catalog pin as the recording and its only reference", ac.Identity)
	}
}

func TestResolveIdentity_PinnedTrackSurvivesAResolverFailure(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{err: errors.New("musicbrainz down")}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry {
		t.Errorf("identity = %+v, want the pin despite the failed resolve", ac.Identity)
	}
}

func TestResolveIdentity_ResolvedPinnedIdentityIsAdoptedAsIs(t *testing.T) {
	identifier := &clusterIdentifier{clusters: map[string][]string{pinnedBoysDontCry: {pinnedCluster}}}
	resolved := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry, "isrc-sibling"}, Duration: 157.306}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{identity: resolved}, identifier, ac)

	if !slices.Equal(ac.Identity.MBIDs, resolved.MBIDs) || ac.Identity.Duration != 157.306 || ac.Track.CatalogDuration != 0 {
		t.Errorf("identity = %+v, track duration %v, want the resolver's pinned identity untouched", ac.Identity, ac.Track.CatalogDuration)
	}
	if len(identifier.asked) != 0 {
		t.Errorf("cluster asked for %v, want no cluster lookup", identifier.asked)
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

const inPlaceRef = "u/the cure/boys dont cry.mp3"

type stubPreviewMatcher struct {
	comparison ports.PreviewComparison
	err        error
	targets    []ports.PreviewTarget
}

func (m *stubPreviewMatcher) MatchesPreview(_ context.Context, _ string, target ports.PreviewTarget) (ports.PreviewComparison, error) {
	m.targets = append(m.targets, target)
	return m.comparison, m.err
}

func inPlaceTrack() *ports.TrackSnapshot {
	ref := inPlaceRef
	isrc := "GBAAA8000001"
	seconds := 155.0
	return &ports.TrackSnapshot{
		ID: shared.TrackIdFromUUID(uuid.New()), Title: "Boys Don't Cry", Artist: "The Cure",
		AudioRef: &ref, ISRC: &isrc, StoredDurationSeconds: &seconds,
		CatalogSources: []ports.CatalogSource{{Provider: "deezer", ExternalID: "913"}},
	}
}

func inPlaceMatch(recordings ...ports.LinkedRecording) ports.RecordingMatch {
	return ports.RecordingMatch{AcoustID: "ac-1", Results: []ports.AcoustIDResult{{ID: "ac-1", Score: 0.97, Recordings: recordings}}}
}

func inPlaceService(identifier ports.AudioIdentifier, previews ports.PreviewMatcher, mbid string) *AcquireTrackAudioService {
	store := &bytesAudioStore{objects: map[string]string{inPlaceRef: "stored bytes"}}
	return NewAcquireTrackAudioService(nil, nil, store,
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: mbid, MBIDs: []string{mbid}}}),
		WithAudioIdentifier(identifier),
		WithStoredAudioReader(storedBytesReader{store: store}),
		WithPreviewMatcher(previews))
}

func TestCheckInPlace_AFileTheFingerprintLinksToTheBoundRecordingMatches(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: pinnedBoysDontCry, Title: "Boys Don't Cry", Artists: []string{"The Cure"}})}

	finding, err := inPlaceService(identifier, nil, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Verdict != string(VerdictHard) || finding.Basis != "acoustid" || finding.Score != 0.97 {
		t.Errorf("finding = %+v, want a hard acoustid match at 0.97", finding)
	}
}

func TestCheckInPlace_AFileTheFingerprintLinksToAnotherVersionContradicts(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "live-mbid", Title: "Boys Don't Cry (Live)", Artists: []string{"The Cure"}})}

	finding, err := inPlaceService(identifier, nil, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Contradicts || finding.Verdict != string(VerdictOtherVersion) {
		t.Errorf("finding = %+v, want the other version to contradict", finding)
	}
}

func TestCheckInPlace_WithNoAcoustIDAnswerThePreviewComparisonDecides(t *testing.T) {
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.05}}

	finding, err := inPlaceService(&stubIdentifier{}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Basis != "preview" {
		t.Errorf("finding = %+v, want a match on the preview basis", finding)
	}
	want := ports.PreviewTarget{ISRC: "GBAAA8000001", DeezerTrackID: "913"}
	if len(previews.targets) != 1 || previews.targets[0] != want {
		t.Errorf("preview targets = %+v, want the track's ISRC and Deezer id %+v", previews.targets, want)
	}
}

func TestCheckInPlace_APreviewThatDoesNotMatchOrCannotBeFetchedLeavesTheFileUnanswered(t *testing.T) {
	cases := map[string]*stubPreviewMatcher{
		"the preview is another song": {comparison: ports.PreviewComparison{BitErrorRate: 0.5}},
		"deezer has no preview":       {comparison: ports.PreviewComparison{Unavailable: "deezer has no preview for this track"}},
		"deezer is down":              {err: errors.New("deezer unreachable")},
	}
	for name, previews := range cases {
		t.Run(name, func(t *testing.T) {
			finding, err := inPlaceService(&stubIdentifier{}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())

			if err != nil || finding.Answered() {
				t.Errorf("finding = %+v, err = %v, want no answer and no error", finding, err)
			}
		})
	}
}

func TestCheckInPlace_ATrackWithNoBoundRecordingIsOnlyComparedByPreview(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: pinnedBoysDontCry})}
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true}}

	finding, err := inPlaceService(identifier, previews, "").CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if identifier.calls != 0 || finding.Basis != "preview" {
		t.Errorf("identify calls = %d, finding = %+v, want AcoustID skipped without a bound recording", identifier.calls, finding)
	}
}

func TestCheckInPlace_AFailedFingerprintIsAnErrorNotAnAnswer(t *testing.T) {
	outage := errors.New("acoustid unreachable")

	_, err := inPlaceService(&stubIdentifier{err: outage}, &stubPreviewMatcher{}, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())

	if !errors.Is(err, outage) {
		t.Errorf("CheckInPlace = %v, want the outage", err)
	}
}

func TestCheckInPlace_WithoutAnIdentifierOrStoredAudioReaderItSaysSo(t *testing.T) {
	svc := NewAcquireTrackAudioService(nil, nil, &bytesAudioStore{})

	if _, err := svc.CheckInPlace(context.Background(), inPlaceTrack()); !errors.Is(err, ErrInPlaceUnavailable) {
		t.Errorf("CheckInPlace = %v, want ErrInPlaceUnavailable", err)
	}
}
