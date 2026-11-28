package service_test

import (
	"altune/go-api/internal/download/internal/adapters/inmemoryledger"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

const (
	pinnedBoysDontCry = "2b2af12b-48f1-444e-8591-cacb9f88f02a"
	inPlaceRef        = "u/the cure/boys dont cry.mp3"
)

func identifiedAs(recordings ...ports.LinkedRecording) *identifier {
	return &identifier{match: ports.RecordingMatch{
		AcoustID: "ac-1",
		Results:  []ports.AcoustIDResult{{ID: "ac-1", Score: 0.97, Recordings: recordings}},
	}}
}

func boysDontCry(mbid string) ports.LinkedRecording {
	return ports.LinkedRecording{MBID: mbid, Title: "Boys Don't Cry", Artists: []string{"The Cure"}}
}

func otherRecording(mbid, title string) ports.LinkedRecording {
	return ports.LinkedRecording{MBID: mbid, Title: title, Artists: []string{"The Cure"}}
}

type previewMatcher struct {
	comparison ports.PreviewComparison
	err        error
	targets    []ports.PreviewTarget
}

func (m *previewMatcher) MatchesPreview(_ context.Context, _ string, target ports.PreviewTarget) (ports.PreviewComparison, error) {
	m.targets = append(m.targets, target)
	return m.comparison, m.err
}

func previewAt(bitErrorRate float64) *previewMatcher {
	return &previewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: bitErrorRate, OverlapSeconds: 29}}
}

func judgedPreview(bitErrorRate float64) *previewMatcher {
	return &previewMatcher{comparison: ports.PreviewComparison{Matched: bitErrorRate <= 0.18, BitErrorRate: bitErrorRate, OverlapSeconds: 29}}
}

type versionLookup struct {
	facts map[string]ports.RecordingFacts
}

func (l versionLookup) RecordingByMBID(_ context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	facts, found := l.facts[mbid]
	return facts, found, nil
}

func liveVersionLookup() versionLookup {
	return versionLookup{facts: map[string]ports.RecordingFacts{
		"live-mbid": {MBID: "live-mbid", Title: "Boys Don't Cry", Disambiguation: "live"},
	}}
}

type inPlaceWiring struct {
	identifier ports.AudioIdentifier
	resolver   *resolver
	previews   ports.PreviewMatcher
	lookup     ports.RecordingLookup
	provenCut  float64
}

func inPlaceChecker(w inPlaceWiring) *service.InPlaceChecker {
	if w.resolver == nil {
		w.resolver = resolvedTo(pinnedBoysDontCry)
	}
	if w.provenCut == 0 {
		w.provenCut = 0.10
	}
	wiring := resolving(w.resolver, noVerification())
	if w.identifier != nil {
		wiring.Identifier = w.identifier
	}
	if w.previews != nil {
		wiring.Previews = w.previews
	}
	if w.lookup != nil {
		wiring.RecordingLookup = w.lookup
	}
	return service.NewInPlaceChecker(service.MustNewAcquireTrackAudioService(inmemoryledger.NewLedger(), service.NewSourceRegistry(), storedAudio{}, scratchfiles.New(), wiring, noStages(readingStoredFrom(storedAudio{})),
		service.WithProvenMaxBitErrorRate(w.provenCut)))
}

func inPlaceTrack() *ports.TrackSnapshot {
	ref, isrc, seconds := inPlaceRef, "GBAAA8000001", 155.0
	return &ports.TrackSnapshot{
		ID: ids.TrackIdFromUUID(uuid.New()), Title: "Boys Don't Cry", Artist: "The Cure",
		AudioRef: &ref, ISRC: &isrc, StoredDurationSeconds: &seconds,
		CatalogSources: []ports.CatalogSource{{Provider: "deezer", ExternalID: "913"}},
	}
}

func pinnedInPlaceTrack() *ports.TrackSnapshot {
	track := inPlaceTrack()
	pin := pinnedBoysDontCry
	track.CatalogMBID = &pin
	return track
}

func fromSource(track *ports.TrackSnapshot, evidence string) *ports.TrackSnapshot {
	url := "https://music.youtube.com/watch?v=artAAAAAAAA"
	track.AudioSourceURL = &url
	track.Evidence = []byte(evidence)
	return track
}

func checkInPlace(t *testing.T, w inPlaceWiring, track *ports.TrackSnapshot) replacement.Finding {
	t.Helper()
	finding, err := inPlaceChecker(w).CheckInPlace(context.Background(), track)
	if err != nil {
		t.Fatalf("CheckInPlace: %v", err)
	}
	return finding
}

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

func TestCheckInPlace_APinTheResolverKnowsNothingAboutIsStillTheFingerprintReference(t *testing.T) {
	finding := checkInPlace(t, inPlaceWiring{identifier: identifiedAs(boysDontCry(pinnedBoysDontCry)), resolver: &resolver{}}, pinnedInPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Basis != "acoustid" {
		t.Errorf("finding = %+v, want the catalog pin to be the recording the fingerprint matches", finding)
	}
}

func TestCheckInPlace_ThePinIsTheOnlyReferenceWhenTheResolverKnowsNothing(t *testing.T) {
	finding := checkInPlace(t, inPlaceWiring{identifier: identifiedAs(boysDontCry("isrc-sibling")), resolver: &resolver{}}, pinnedInPlaceTrack())

	if finding.Fingerprint == audioquality.Matches {
		t.Errorf("finding = %+v, want a recording other than the pin not to match", finding)
	}
}

func TestCheckInPlace_APinSurvivesAResolverFailure(t *testing.T) {
	wiring := inPlaceWiring{identifier: identifiedAs(boysDontCry(pinnedBoysDontCry)), resolver: &resolver{err: errors.New("musicbrainz down")}}

	finding := checkInPlace(t, wiring, pinnedInPlaceTrack())

	if finding.Fingerprint != audioquality.Matches {
		t.Errorf("finding = %+v, want the pin to stand despite the failed resolve", finding)
	}
}

func TestCheckInPlace_AResolvedPinnedIdentityIsAdoptedWithItsSiblings(t *testing.T) {
	resolved := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry, "isrc-sibling"}, Duration: 157.306}
	wiring := inPlaceWiring{identifier: identifiedAs(boysDontCry("isrc-sibling")), resolver: &resolver{identity: resolved}}

	finding := checkInPlace(t, wiring, pinnedInPlaceTrack())

	if finding.Fingerprint != audioquality.Matches {
		t.Errorf("finding = %+v, want the resolver's sibling recording to count as the track", finding)
	}
}

func TestCheckInPlace_AnUnpinnedTrackWithNothingResolvedIsNeverFingerprinted(t *testing.T) {
	fingerprints := identifiedAs(boysDontCry(pinnedBoysDontCry))

	checkInPlace(t, inPlaceWiring{identifier: fingerprints, resolver: &resolver{}}, inPlaceTrack())

	if fingerprints.calls != 0 {
		t.Errorf("identify calls = %d, want none without a recording to compare against", fingerprints.calls)
	}
}

func TestCheckInPlace_TheResolverIsAskedForTheCatalogPin(t *testing.T) {
	asked := &resolver{}

	checkInPlace(t, inPlaceWiring{identifier: &identifier{}, resolver: asked}, pinnedInPlaceTrack())

	if len(asked.queries) != 1 || asked.queries[0].RecordingMBID != pinnedBoysDontCry {
		t.Errorf("queries = %+v, want one carrying the catalog pin", asked.queries)
	}
}

func TestCheckInPlace_AFileTheFingerprintLinksToTheBoundRecordingMatches(t *testing.T) {
	previews := previewAt(0.05)

	finding := checkInPlace(t, inPlaceWiring{identifier: identifiedAs(boysDontCry(pinnedBoysDontCry)), previews: previews}, inPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Verdict != string(service.VerdictHard) || finding.Basis != "acoustid" || finding.Score != 0.97 {
		t.Errorf("finding = %+v, want a hard acoustid match at 0.97", finding)
	}
	if len(previews.targets) != 0 {
		t.Errorf("preview fetched %d times, want none once the fingerprint proves the file", len(previews.targets))
	}
}

func TestCheckInPlace_AFileTheFingerprintLinksToAnotherVersionContradicts(t *testing.T) {
	wiring := inPlaceWiring{
		identifier: identifiedAs(otherRecording("live-mbid", "Boys Don't Cry (Live)")),
		lookup:     liveVersionLookup(),
	}
	live := fromSource(inPlaceTrack(), `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)"}`)

	finding := checkInPlace(t, wiring, live)

	if finding.Fingerprint != audioquality.Contradicts || finding.Verdict != string(service.VerdictContradicted) || !finding.Lowers {
		t.Errorf("finding = %+v, want the other version to contradict", finding)
	}
}

func TestCheckInPlace_UnreadableStoredEvidenceJudgesLikeNoEvidence(t *testing.T) {
	wiring := func() inPlaceWiring {
		return inPlaceWiring{
			identifier: identifiedAs(otherRecording("live-mbid", "Boys Don't Cry (Live)")),
			lookup:     liveVersionLookup(),
		}
	}

	logs := logging.CaptureRing(t, "debug")
	track := fromSource(inPlaceTrack(), `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)","pipeline_generation":"x"}`)

	unreadable := checkInPlace(t, wiring(), track)
	absent := checkInPlace(t, wiring(), fromSource(inPlaceTrack(), ``))

	if unreadable != absent {
		t.Errorf("unreadable evidence gave %+v, want the same finding as absent evidence %+v: nothing it half-decoded may be used", unreadable, absent)
	}
	records := recordsNamed(logs, "acquisition.stored_evidence_unreadable")
	if len(records) != 1 || records[0].Level != "ERROR" || records[0].Attrs["track_id"] != track.ID.String() {
		t.Errorf("records = %+v, want one ERROR acquisition.stored_evidence_unreadable naming track %s", records, track.ID)
	}
}

func TestCheckInPlace_WithNoAcoustIDAnswerThePreviewComparisonDecides(t *testing.T) {
	previews := previewAt(0.05)

	finding := checkInPlace(t, inPlaceWiring{identifier: &identifier{}, previews: previews}, inPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Basis != "preview" || finding.Verdict != "preview" || finding.BitErrorRate != 0.05 {
		t.Errorf("finding = %+v, want a match on the preview basis with the preview verdict and the bit error rate", finding)
	}
	want := ports.PreviewTarget{ISRC: "GBAAA8000001", DeezerTrackID: "913"}
	if len(previews.targets) != 1 || previews.targets[0] != want {
		t.Errorf("preview targets = %+v, want the track's ISRC and Deezer id %+v", previews.targets, want)
	}
}

func TestCheckInPlace_APreviewMatchAboveTheOfficialCutOnlyApproximates(t *testing.T) {
	finding := checkInPlace(t, inPlaceWiring{identifier: &identifier{}, previews: previewAt(0.15)}, inPlaceTrack())

	if finding.Fingerprint != audioquality.Approximates || finding.Verdict != "preview" {
		t.Errorf("finding = %+v, want a loose preview match to approximate", finding)
	}
}

func TestCheckInPlace_APreviewMatchOnADoubtedReferenceOnlyApproximates(t *testing.T) {
	doubted := ports.RecordingIdentity{MBID: pinnedBoysDontCry, MBIDs: []string{pinnedBoysDontCry}, ReferenceDoubted: true}
	wiring := inPlaceWiring{identifier: &identifier{}, resolver: &resolver{identity: doubted}, previews: previewAt(0.05)}

	finding := checkInPlace(t, wiring, inPlaceTrack())

	if finding.Fingerprint != audioquality.Approximates {
		t.Errorf("finding = %+v, want a doubted reference to cap the preview at approximate", finding)
	}
}

func TestCheckInPlace_AContradictingAcoustIDAnswerWithAMatchingPreviewProvesNothingWrong(t *testing.T) {
	wiring := inPlaceWiring{identifier: identifiedAs(otherRecording("part-one-mbid", "Boys Don't Cry (Part 2)")), previews: previewAt(0.06)}

	finding := checkInPlace(t, wiring, inPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Verdict != "preview" {
		t.Errorf("finding = %+v, want the matching preview to outweigh the contradicting AcoustID answer", finding)
	}
}

func TestCheckInPlace_AContradictionStandsOnlyWhenThePreviewDoesNotMatchEither(t *testing.T) {
	cases := map[string]*previewMatcher{
		"the preview is another song": {comparison: ports.PreviewComparison{BitErrorRate: 0.5, OverlapSeconds: 29}},
		"deezer has no preview":       {comparison: ports.PreviewComparison{Unavailable: "deezer has no preview for this track"}},
	}
	for name, previews := range cases {
		t.Run(name, func(t *testing.T) {
			wiring := inPlaceWiring{
				identifier: identifiedAs(otherRecording("live-mbid", "Boys Don't Cry (Live)")),
				previews:   previews,
				lookup:     liveVersionLookup(),
			}
			live := fromSource(inPlaceTrack(), `{"source_kind":"art_track","source_title":"Boys Don't Cry (Live)"}`)

			if finding := checkInPlace(t, wiring, live); finding.Fingerprint != audioquality.Contradicts {
				t.Errorf("finding = %+v, want the contradiction to stand", finding)
			}
		})
	}
}

func TestCheckInPlace_ADifferentSongAnswerWithAMatchingPreviewIsNotAMismatch(t *testing.T) {
	wiring := inPlaceWiring{identifier: identifiedAs(otherRecording("other-song", "Killing an Arab")), previews: previewAt(0.07)}

	finding := checkInPlace(t, wiring, inPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Basis != "preview" {
		t.Errorf("finding = %+v, want the preview match to settle it", finding)
	}
}

func TestCheckInPlace_APreviewThatDoesNotMatchOrCannotBeFetchedLeavesTheFileUnanswered(t *testing.T) {
	cases := map[string]*previewMatcher{
		"the preview is another song": {comparison: ports.PreviewComparison{BitErrorRate: 0.5}},
		"deezer has no preview":       {comparison: ports.PreviewComparison{Unavailable: "deezer has no preview for this track"}},
		"deezer is down":              {err: errors.New("deezer unreachable")},
		"deezer is throttling":        {err: fmt.Errorf("fetch the preview: %w", errors.New("deezer is still throttling after retries"))},
		"an unavailable preview whatever it reports": {comparison: ports.PreviewComparison{
			Unavailable: "deezer has no preview for this track", Matched: true, BitErrorRate: 0.02, OverlapSeconds: 29,
		}},
	}
	for name, previews := range cases {
		t.Run(name, func(t *testing.T) {
			if finding := checkInPlace(t, inPlaceWiring{identifier: &identifier{}, previews: previews}, inPlaceTrack()); finding.Answered() {
				t.Errorf("finding = %+v, want no answer and no error", finding)
			}
		})
	}
}

func TestCheckInPlace_ATrackWithNoBoundRecordingIsOnlyComparedByPreview(t *testing.T) {
	fingerprints := identifiedAs(boysDontCry(pinnedBoysDontCry))
	wiring := inPlaceWiring{identifier: fingerprints, resolver: resolvedTo(""), previews: &previewMatcher{comparison: ports.PreviewComparison{Matched: true, OverlapSeconds: 29}}}

	finding := checkInPlace(t, wiring, inPlaceTrack())

	if fingerprints.calls != 0 || finding.Basis != "preview" {
		t.Errorf("identify calls = %d, finding = %+v, want AcoustID skipped without a bound recording", fingerprints.calls, finding)
	}
}

func TestCheckInPlace_AFailedFingerprintIsAnErrorNotAnAnswer(t *testing.T) {
	outage := errors.New("acoustid unreachable")

	_, err := inPlaceChecker(inPlaceWiring{identifier: &identifier{err: outage}, previews: &previewMatcher{}}).CheckInPlace(context.Background(), inPlaceTrack())

	if !errors.Is(err, outage) {
		t.Errorf("CheckInPlace = %v, want the outage", err)
	}
}

func TestCheckInPlace_WithoutAnIdentifierOrStoredAudioReaderItSaysSo(t *testing.T) {
	svc := service.MustNewAcquireTrackAudioService(inmemoryledger.NewLedger(), service.NewSourceRegistry(), storedAudio{}, scratchfiles.New(), noVerification(), noStages())

	finding, err := service.NewInPlaceChecker(svc).CheckInPlace(context.Background(), inPlaceTrack())

	if !errors.Is(err, service.ErrInPlaceUnavailable) || finding.Answered() {
		t.Errorf("CheckInPlace = %+v, %v, want ErrInPlaceUnavailable", finding, err)
	}
}

func TestCheckInPlace_APreviewAtTheProvenCutGradesAOnAnOfficialSourceAndBOnAFanUpload(t *testing.T) {
	cases := map[string]struct {
		evidence string
		want     audioquality.FingerprintResult
	}{
		"art track":  {`{"source_kind":"art_track"}`, audioquality.Matches},
		"fan upload": {`{"source_kind":"fan_upload"}`, audioquality.Approximates},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			finding := checkInPlace(t, inPlaceWiring{identifier: &identifier{}, previews: judgedPreview(0.05)}, fromSource(inPlaceTrack(), tc.evidence))

			if finding.Fingerprint != tc.want || finding.Basis != "preview" || finding.Lowers {
				t.Errorf("finding = %+v, want fingerprint %v on the preview basis and no lowering", finding, tc.want)
			}
		})
	}
}

func TestCheckInPlace_TheProvenCutIsTheOneTheServiceWasGiven(t *testing.T) {
	cases := map[float64]audioquality.FingerprintResult{0.15: audioquality.Matches, 0.10: audioquality.Approximates}
	for provenCut, want := range cases {
		t.Run(fmt.Sprint(provenCut), func(t *testing.T) {
			wiring := inPlaceWiring{identifier: &identifier{}, previews: judgedPreview(0.12), provenCut: provenCut}

			if finding := checkInPlace(t, wiring, fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`)); finding.Fingerprint != want {
				t.Errorf("proven cut %v: finding = %+v, want fingerprint %v", provenCut, finding, want)
			}
		})
	}
}

func TestCheckInPlace_AnAcoustIDAnswerNamingAnotherRecordingIsOneSourceAndNeverLowersAGrade(t *testing.T) {
	wiring := inPlaceWiring{identifier: identifiedAs(otherRecording("live-mbid", "Killing an Arab"))}

	finding := checkInPlace(t, wiring, fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`))

	if finding.Answered() || finding.Lowers {
		t.Errorf("finding = %+v, want no answer: unknown never lowers a grade", finding)
	}
}

func TestCheckInPlace_AContradictionNeedsASecondIndependentSource(t *testing.T) {
	cases := map[string]struct {
		previews ports.PreviewMatcher
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
			wiring := inPlaceWiring{identifier: identifiedAs(otherRecording("other-mbid", "Killing an Arab")), previews: tc.previews}

			finding := checkInPlace(t, wiring, fromSource(inPlaceTrack(), tc.evidence))

			if contradicted := finding.Fingerprint == audioquality.Contradicts && finding.Lowers; contradicted != tc.want {
				t.Errorf("finding = %+v, want contradicted = %v", finding, tc.want)
			}
		})
	}
}

func TestCheckInPlace_AWrongLengthFileTheFingerprintProvesLowersTheGradeBelowA(t *testing.T) {
	track := fromSource(inPlaceTrack(), `{"source_kind":"art_track"}`)
	catalog, stored := 155.0, 262.0
	track.CatalogDurationSeconds, track.StoredDurationSeconds = &catalog, &stored

	finding := checkInPlace(t, inPlaceWiring{identifier: identifiedAs(boysDontCry(pinnedBoysDontCry))}, track)

	if finding.Fingerprint != audioquality.Approximates || !finding.Lowers {
		t.Errorf("finding = %+v, want a proven file of the wrong length to approximate and lower the stored grade", finding)
	}
}

func TestCheckInPlace_APreviewOverlapUnderTheMinimumIsNoPreview(t *testing.T) {
	short := &previewMatcher{comparison: ports.PreviewComparison{Matched: true, BitErrorRate: 0.02, OverlapSeconds: 8}}

	if finding := checkInPlace(t, inPlaceWiring{identifier: &identifier{}, previews: short}, inPlaceTrack()); finding.Answered() {
		t.Errorf("finding = %+v, want no answer from a preview that overlaps too little", finding)
	}
}

func TestCheckInPlace_AnEmptyFingerprintStillFallsBackToThePreviewComparison(t *testing.T) {
	empty := &identifier{err: fmt.Errorf("stored.mp3: %w", domain.ErrEmptyFingerprint)}

	finding := checkInPlace(t, inPlaceWiring{identifier: empty, previews: previewAt(0.05)}, inPlaceTrack())

	if finding.Fingerprint != audioquality.Matches || finding.Basis != "preview" {
		t.Errorf("finding = %+v, want the preview comparison to decide", finding)
	}
}
