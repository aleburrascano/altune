package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
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

type identifyNothing struct{}

func (identifyNothing) Identify(context.Context, string, float64) (ports.RecordingMatch, error) {
	return ports.RecordingMatch{}, nil
}

const pinnedBoysDontCry = "2b2af12b-48f1-444e-8591-cacb9f88f02a"

func pinnedTrack() TrackRef {
	return TrackRef{ID: "t1", Title: "Boys Don't Cry", Artist: "The Cure", CatalogMBID: pinnedBoysDontCry}
}

func TestResolveIdentity_PinnedTrackTheResolverKnowsNothingAboutStillGetsTheFingerprintGate(t *testing.T) {
	identifier := identifyNothing{}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry || !slices.Equal(ac.Identity.MBIDs, []string{pinnedBoysDontCry}) {
		t.Errorf("identity = %+v, want the catalog pin as the recording and its only reference", ac.Identity)
	}
}

func TestResolveIdentity_PinnedTrackSurvivesAResolverFailure(t *testing.T) {
	identifier := identifyNothing{}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{err: errors.New("musicbrainz down")}, identifier, ac)

	if ac.Identity.MBID != pinnedBoysDontCry {
		t.Errorf("identity = %+v, want the pin despite the failed resolve", ac.Identity)
	}
}

func TestResolveIdentity_ResolvedPinnedIdentityIsAdoptedAsIs(t *testing.T) {
	identifier := identifyNothing{}
	resolved := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry, "isrc-sibling"}, Duration: 157.306}
	ac := &AcquisitionContext{Track: pinnedTrack()}

	ResolveIdentity(context.Background(), &stubResolver{identity: resolved}, identifier, ac)

	if !slices.Equal(ac.Identity.MBIDs, resolved.MBIDs) || ac.Identity.Duration != 157.306 || ac.Track.CatalogDuration != 0 {
		t.Errorf("identity = %+v, track duration %v, want the resolver's pinned identity untouched", ac.Identity, ac.Track.CatalogDuration)
	}
}

func TestResolveIdentity_UnpinnedTrackWithNothingResolvedStaysWithoutIdentity(t *testing.T) {
	identifier := identifyNothing{}
	ac := &AcquisitionContext{Track: TrackRef{ID: "t1", Title: "Boys Don't Cry", Artist: "The Cure"}}

	ResolveIdentity(context.Background(), &stubResolver{}, identifier, ac)

	if !ac.Identity.IsZero() {
		t.Errorf("identity = %+v, want no identity without a pin", ac.Identity)
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

func inPlaceService(identifier ports.AudioIdentifier, previews ports.PreviewMatcher, mbid string, extra ...func(*AcquireTrackAudioService)) *AcquireTrackAudioService {
	store := &bytesAudioStore{objects: map[string]string{inPlaceRef: "stored bytes"}}
	opts := []func(*AcquireTrackAudioService){
		WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: mbid, MBIDs: []string{mbid}}}),
		WithAudioIdentifier(identifier),
		WithStoredAudioReader(storedBytesReader{store: store}),
		WithPreviewMatcher(previews, 0.10),
	}
	return NewAcquireTrackAudioService(nil, nil, store, append(opts, extra...)...)
}

func liveRecordingLookup() *stubVersionLookup {
	return &stubVersionLookup{facts: map[string]ports.RecordingFacts{"live-mbid": {MBID: "live-mbid", Title: "Boys Don't Cry", Disambiguation: "live"}}}
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
	liveSource := fromSource(inPlaceTrack(), `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)"}`)

	finding, err := inPlaceService(identifier, nil, pinnedBoysDontCry, WithAcquireRecordingLookup(liveRecordingLookup())).CheckInPlace(context.Background(), liveSource)
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Contradicts || finding.Verdict != string(VerdictContradicted) || !finding.Lowers {
		t.Errorf("finding = %+v, want the other version to contradict", finding)
	}
}

func TestCheckInPlace_WithNoAcoustIDAnswerThePreviewComparisonDecides(t *testing.T) {
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.05, OverlapSeconds: 29}}

	finding, err := inPlaceService(&stubIdentifier{}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Basis != "preview" || finding.Verdict != "preview" || finding.BitErrorRate != 0.05 {
		t.Errorf("finding = %+v, want a match on the preview basis with the preview verdict and the bit error rate", finding)
	}
	want := ports.PreviewTarget{ISRC: "GBAAA8000001", DeezerTrackID: "913"}
	if len(previews.targets) != 1 || previews.targets[0] != want {
		t.Errorf("preview targets = %+v, want the track's ISRC and Deezer id %+v", previews.targets, want)
	}
}

func TestCheckInPlace_APreviewMatchAboveTheOfficialCutOnlyApproximates(t *testing.T) {
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.15, OverlapSeconds: 29}}

	finding, err := inPlaceService(&stubIdentifier{}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Approximates || finding.Verdict != "preview" {
		t.Errorf("finding = %+v, want a loose preview match to approximate", finding)
	}
}

func TestCheckInPlace_APreviewMatchOnADoubtedReferenceOnlyApproximates(t *testing.T) {
	store := &bytesAudioStore{objects: map[string]string{inPlaceRef: "stored bytes"}}
	doubted := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry}, ReferenceDoubted: true}
	svc := NewAcquireTrackAudioService(nil, nil, store,
		WithRecordingResolver(&stubResolver{identity: doubted}),
		WithAudioIdentifier(&stubIdentifier{}),
		WithStoredAudioReader(storedBytesReader{store: store}),
		WithPreviewMatcher(&stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.05, OverlapSeconds: 29}}, 0.10))

	finding, err := svc.CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Approximates {
		t.Errorf("finding = %+v, want a doubted reference to cap the preview at approximate", finding)
	}
}

func TestCheckInPlace_AContradictingAcoustIDAnswerWithAMatchingPreviewProvesNothingWrong(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "part-one-mbid", Title: "Boys Don't Cry (Part 2)", Artists: []string{"The Cure"}})}
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.06, OverlapSeconds: 29}}

	finding, err := inPlaceService(identifier, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Verdict != "preview" {
		t.Errorf("finding = %+v, want the matching preview to outweigh the contradicting AcoustID answer", finding)
	}
}

func TestCheckInPlace_AContradictionStandsOnlyWhenThePreviewDoesNotMatchEither(t *testing.T) {
	cases := map[string]*stubPreviewMatcher{
		"the preview is another song": {comparison: ports.PreviewComparison{BitErrorRate: 0.5, OverlapSeconds: 29}},
		"deezer has no preview":       {comparison: ports.PreviewComparison{Unavailable: "deezer has no preview for this track"}},
	}
	for name, previews := range cases {
		t.Run(name, func(t *testing.T) {
			identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "live-mbid", Title: "Boys Don't Cry (Live)", Artists: []string{"The Cure"}})}
			liveSource := fromSource(inPlaceTrack(), `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)"}`)

			finding, err := inPlaceService(identifier, previews, pinnedBoysDontCry, WithAcquireRecordingLookup(liveRecordingLookup())).CheckInPlace(context.Background(), liveSource)
			if err != nil {
				t.Fatalf("CheckInPlace: %v", err)
			}
			if finding.Fingerprint != grade.Contradicts {
				t.Errorf("finding = %+v, want the contradiction to stand", finding)
			}
		})
	}
}

func TestCheckInPlace_ADifferentSongAnswerWithAMatchingPreviewIsNotAMismatch(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "other-song", Title: "Killing an Arab", Artists: []string{"The Cure"}})}
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.07, OverlapSeconds: 29}}

	finding, err := inPlaceService(identifier, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Basis != "preview" {
		t.Errorf("finding = %+v, want the preview match to settle it", finding)
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
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, OverlapSeconds: 29}}

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

func fromSource(track *ports.TrackSnapshot, evidence string) *ports.TrackSnapshot {
	url := "https://music.youtube.com/watch?v=art"
	track.AudioSourceURL = &url
	track.Evidence = []byte(evidence)
	return track
}

func judgedPreview(rate float64) *stubPreviewMatcher {
	return &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: rate <= 0.18, BitErrorRate: rate, OverlapSeconds: 29}}
}

func TestCheckInPlace_APreviewAtTheProvenCutGradesAOnAnOfficialSourceAndBOnAFanUpload(t *testing.T) {
	cases := map[string]struct {
		evidence string
		want     grade.Fingerprint
	}{
		"art track":  {`{"source_kind":"art_track"}`, grade.Matches},
		"fan upload": {`{"source_kind":"fan_upload"}`, grade.Approximates},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			track := fromSource(inPlaceTrack(), tc.evidence)

			finding, err := inPlaceService(&stubIdentifier{}, judgedPreview(0.05), pinnedBoysDontCry).CheckInPlace(context.Background(), track)

			if err != nil || finding.Fingerprint != tc.want || finding.Basis != "preview" || finding.Lowers {
				t.Errorf("finding = %+v, err = %v, want fingerprint %v on the preview basis and no lowering", finding, err, tc.want)
			}
		})
	}
}

func TestCheckInPlace_TheProvenCutIsTheOneTheServiceWasGiven(t *testing.T) {
	cases := map[float64]grade.Fingerprint{0.15: grade.Matches, 0.10: grade.Approximates}
	for provenMax, want := range cases {
		t.Run("", func(t *testing.T) {
			store := &bytesAudioStore{objects: map[string]string{inPlaceRef: "stored bytes"}}
			svc := NewAcquireTrackAudioService(nil, nil, store,
				WithRecordingResolver(&stubResolver{identity: ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry}}}),
				WithAudioIdentifier(&stubIdentifier{}),
				WithStoredAudioReader(storedBytesReader{store: store}),
				WithPreviewMatcher(judgedPreview(0.12), provenMax))

			finding, err := svc.CheckInPlace(context.Background(), fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`))

			if err != nil || finding.Fingerprint != want {
				t.Errorf("proven cut %v: finding = %+v, err = %v, want fingerprint %v", provenMax, finding, err, want)
			}
		})
	}
}

func TestCheckInPlace_AnAcoustIDAnswerNamingAnotherRecordingIsOneSourceAndNeverLowersAGrade(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: "live-mbid", Title: "Killing an Arab", Artists: []string{"The Cure"}})}

	finding, err := inPlaceService(identifier, nil, pinnedBoysDontCry).CheckInPlace(context.Background(), fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`))

	if err != nil || finding.Answered() || finding.Lowers {
		t.Errorf("finding = %+v, err = %v, want no answer: unknown never lowers a grade", finding, err)
	}
}

func TestCheckInPlace_AContradictionNeedsASecondIndependentSource(t *testing.T) {
	other := inPlaceMatch(ports.LinkedRecording{MBID: "other-mbid", Title: "Killing an Arab", Artists: []string{"The Cure"}})
	cases := map[string]struct {
		previews *stubPreviewMatcher
		evidence string
		want     bool
	}{
		"an absent preview":                      {judgedPreview(0.45), `{"source_kind":"art_track"}`, true},
		"a source title with other version tags": {nil, `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)"}`, true},
		"a preview that is between the cuts":     {judgedPreview(0.25), `{"source_kind":"art_track"}`, false},
		"a source title with the same tags":      {nil, `{"source_kind":"art_track","source_title":"Boys Don't Cry (Remastered)"}`, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var previews ports.PreviewMatcher
			if tc.previews != nil {
				previews = tc.previews
			}

			finding, err := inPlaceService(&stubIdentifier{match: other}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), fromSource(inPlaceTrack(), tc.evidence))

			contradicted := finding.Fingerprint == grade.Contradicts && finding.Lowers
			if err != nil || contradicted != tc.want {
				t.Errorf("finding = %+v, err = %v, want contradicted = %v", finding, err, tc.want)
			}
		})
	}
}

func TestCheckInPlace_AWrongLengthFileTheFingerprintProvesLowersTheGradeBelowA(t *testing.T) {
	identifier := &stubIdentifier{match: inPlaceMatch(ports.LinkedRecording{MBID: pinnedBoysDontCry, Title: "Boys Don't Cry", Artists: []string{"The Cure"}})}
	track := fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`)
	catalog, stored := 155.0, 262.0
	track.CatalogDurationSeconds, track.StoredDurationSeconds = &catalog, &stored

	finding, err := inPlaceService(identifier, nil, pinnedBoysDontCry).CheckInPlace(context.Background(), track)

	if err != nil || finding.Fingerprint != grade.Approximates || !finding.Lowers {
		t.Errorf("finding = %+v, err = %v, want a proven file of the wrong length to approximate and lower the stored grade", finding, err)
	}
}

func TestCheckInPlace_APreviewOverlapUnderTheMinimumIsNoPreview(t *testing.T) {
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.02, OverlapSeconds: 8}}

	finding, err := inPlaceService(&stubIdentifier{}, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())

	if err != nil || finding.Answered() {
		t.Errorf("finding = %+v, err = %v, want no answer from a preview that overlaps too little", finding, err)
	}
}

func TestCheckInPlace_AnEmptyFingerprintStillFallsBackToThePreviewComparison(t *testing.T) {
	previews := &stubPreviewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.05, OverlapSeconds: 29}}
	identifier := &stubIdentifier{err: fmt.Errorf("stored.mp3: %w", acqdomain.ErrEmptyFingerprint)}

	finding, err := inPlaceService(identifier, previews, pinnedBoysDontCry).CheckInPlace(context.Background(), inPlaceTrack())
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	if finding.Fingerprint != grade.Matches || finding.Basis != "preview" {
		t.Errorf("finding = %+v, want the preview comparison to decide", finding)
	}
}
