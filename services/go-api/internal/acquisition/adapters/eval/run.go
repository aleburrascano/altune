package eval

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/adapters/discoverybridge"
	"altune/go-api/internal/acquisition/adapters/tempfs"
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
)

type Outcome struct {
	Case             Case
	TopRanked        string
	Stored           string
	Failed           bool
	Err              string
	Pass             bool
	Reason           string
	Pending          bool
	SimulatedSeconds float64
	Attempts         int
	Provenance       string
	FailureCode      string
	Saved            SavedTrack
}

func Run(ctx context.Context, kase Case) Outcome {
	p := newCasePorts(kase)
	ledger, err := catalogbridge.NewSeededLedger(seedFor(kase))
	if err != nil {
		return seedFailure(kase, err)
	}

	var observed *service.AcquisitionContext
	svc := service.NewAcquireTrackAudioService(ledger, p.registry(), p,
		service.WithAudioProber(p),
		service.WithAudioIdentifier(p),
		service.WithRecordingResolver(resolverFor(kase)),
		service.WithAcquireRecordingLookup(recordedLookups(kase.Recordings)),
		service.WithAcquireTempWorkspace(tempfs.New()),
		service.WithStoredAudioReader(p),
		service.WithContextObserver(func(ac *service.AcquisitionContext) { observed = ac }),
	)
	runErr := executeFor(ctx, svc, kase, ledger)
	if observed == nil && runErr == nil {
		runErr = errors.New("eval: the service never started an acquisition")
	}
	return outcomeOf(kase, observed, runErr, p.clock, savedTrackOf(ledger.Saved()))
}

func executeFor(ctx context.Context, svc *service.AcquireTrackAudioService, kase Case, ledger *catalogbridge.SeededLedger) error {
	if kase.Stored != nil {
		return svc.ExecuteReplace(ctx, ledger.UserID(), ledger.TrackID())
	}
	return svc.Execute(ctx, ledger.UserID(), ledger.TrackID())
}

func seedFailure(kase Case, err error) Outcome {
	return Outcome{
		Case:    kase,
		Failed:  true,
		Err:     err.Error(),
		Pending: kase.isPending(),
		Reason:  "could not seed the catalog track: " + err.Error(),
	}
}

func seedFor(kase Case) catalogbridge.SeededTrack {
	t := kase.Track
	seed := catalogbridge.SeededTrack{
		Title:           t.Title,
		Artist:          t.Artist,
		Album:           t.Album,
		ISRC:            t.ISRC,
		CatalogDuration: targetLength(t),
		CatalogMBID:     t.CatalogMBID,
		CatalogExplicit: t.CatalogExplicit,
		CatalogSources:  t.portCatalogSources(),
	}
	for _, rawURL := range kase.ExcludeURLs {
		seed.RejectedSourceKeys = append(seed.RejectedSourceKeys, acqdomain.SourceKey(rawURL))
	}
	if kase.Stored != nil {
		seed.Stored = &catalogbridge.SeededFile{Duration: kase.Stored.Duration, SourceURL: kase.Stored.SourceURL}
	}
	return seed
}

func resolverFor(kase Case) ports.RecordingResolver {
	res := kase.Track.Resolution
	if res == nil {
		return copiedIdentity(identityFor(kase))
	}
	isrcRecordings := make([]discoverybridge.RecordedISRCRecording, 0, len(res.ISRCRecordings))
	for _, rec := range res.ISRCRecordings {
		isrcRecordings = append(isrcRecordings, discoverybridge.RecordedISRCRecording{MBID: rec.MBID, Duration: rec.Duration})
	}
	return discoverybridge.NewRecordedResolver(searchHitFor(kase), isrcRecordings, pinnedFactsFor(res))
}

func pinnedFactsFor(res *Resolution) *ports.RecordingFacts {
	if res.Pinned == nil {
		return nil
	}
	facts := res.Pinned.facts()
	return &facts
}

func searchHitFor(kase Case) *discoverybridge.RecordedSearchHit {
	search := kase.Track.Resolution.Search
	if search == nil {
		return nil
	}
	return &discoverybridge.RecordedSearchHit{
		Title:    kase.Track.Title,
		Artist:   kase.Track.Artist,
		ISRC:     search.ISRC,
		MBID:     search.MBID,
		Duration: search.Duration,
	}
}

func outcomeOf(kase Case, ac *service.AcquisitionContext, runErr error, clock *simClock, saved SavedTrack) Outcome {
	if ac == nil {
		ac = &service.AcquisitionContext{}
	}
	out := Outcome{
		Case:             kase,
		Failed:           runErr != nil,
		Pending:          kase.isPending(),
		SimulatedSeconds: clock.searchSeconds() + downloadSeconds(kase, ac.Attempted),
		Attempts:         len(ac.Attempted),
		Saved:            saved,
	}
	if runErr != nil {
		out.Err = runErr.Error()
		out.FailureCode = service.FailureCodeOf(runErr)
	}
	if len(ac.Ranked) > 0 {
		out.TopRanked = ac.Ranked[0].URL
	}
	if runErr == nil && ac.Selected != nil {
		out.Stored = ac.Selected.URL
		out.Provenance = string(ac.Provenance())
	}
	out.Pass, out.Reason = judge(kase, out)
	return out
}

func downloadSeconds(kase Case, attempted []service.AttemptedCandidate) float64 {
	slowest := map[int]float64{}
	for _, a := range attempted {
		cand, ok := kase.candidateByURL(a.URL)
		if !ok {
			continue
		}
		slowest[a.Window] = max(slowest[a.Window], cand.downloadSeconds())
	}
	total := 0.0
	for _, seconds := range slowest {
		total += seconds
	}
	return total
}

func RunAll(ctx context.Context, cases []Case) []Outcome {
	outcomes := make([]Outcome, 0, len(cases))
	for _, kase := range cases {
		outcomes = append(outcomes, Run(ctx, kase))
	}
	return outcomes
}

func judge(kase Case, out Outcome) (bool, string) {
	pass, reason := judgeStored(kase, out)
	if !pass {
		return false, reason
	}
	return judgeExpectations(kase, out, reason)
}

func judgeExpectations(kase Case, out Outcome, reason string) (bool, string) {
	if out.Stored != "" && kase.ExpectProvenance != "" && out.Provenance != kase.ExpectProvenance {
		return false, "want provenance " + kase.ExpectProvenance + ", got " + out.Provenance
	}
	if reason, ok := judgeSaved(kase.ExpectSaved, out); !ok {
		return false, reason
	}
	if out.Failed && kase.ExpectFailureCode != "" && out.FailureCode != kase.ExpectFailureCode {
		return false, "want failure code " + kase.ExpectFailureCode + ", got " + out.FailureCode
	}
	return true, reason
}

func judgeSaved(want *SavedExpectation, out Outcome) (string, bool) {
	if want == nil {
		return "", true
	}
	got := out.Saved
	switch {
	case want.ISRC != "" && got.ISRC != want.ISRC:
		return "want saved isrc " + want.ISRC + ", got " + got.ISRC, false
	case want.ISRCSource != "" && got.ISRCSource != want.ISRCSource:
		return "want saved isrc source " + want.ISRCSource + ", got " + got.ISRCSource, false
	case want.Provenance != "" && got.Provenance != want.Provenance:
		return "want saved provenance " + want.Provenance + ", got " + got.Provenance, false
	case len(want.RejectedKeys) > 0 && !slices.Equal(got.RejectedKeys, want.RejectedKeys):
		return fmt.Sprintf("want saved rejected keys %v, got %v", want.RejectedKeys, got.RejectedKeys), false
	case want.DurationSeconds > 0 && got.DurationSeconds != want.DurationSeconds:
		return fmt.Sprintf("want saved duration %v, got %v", want.DurationSeconds, got.DurationSeconds), false
	}
	return "", true
}

func judgeStored(kase Case, out Outcome) (bool, string) {
	if !kase.hasCorrectCandidate() {
		if out.Failed {
			return true, "correctly acquired nothing"
		}
		return false, "stored " + out.Stored + " when no candidate was the right recording"
	}
	if out.Failed {
		return false, "rejected every candidate although a correct one was present"
	}
	stored, ok := kase.candidateByURL(out.Stored)
	if !ok {
		return false, "stored an unknown url " + out.Stored
	}
	if !stored.Correct {
		return false, "stored the wrong recording: " + stored.Title
	}
	return true, "stored the right recording"
}

func identityFor(kase Case) ports.RecordingIdentity {
	return ports.RecordingIdentity{
		ISRC:     kase.Track.ISRC,
		MBID:     kase.Track.MBID,
		Duration: kase.Track.AuthoritativeDuration,
	}
}

func targetLength(t Track) float64 {
	if t.CatalogDuration > 0 {
		return t.CatalogDuration
	}
	return t.Duration
}

type recordedLookups []PinnedRecording

func (l recordedLookups) RecordingByMBID(_ context.Context, mbid string) (ports.RecordingFacts, bool, error) {
	for _, rec := range l {
		if rec.MBID == mbid {
			return rec.facts(), true, nil
		}
	}
	return ports.RecordingFacts{}, false, nil
}

type SavedTrack struct {
	ISRC            string
	ISRCSource      string
	Provenance      string
	DurationSeconds float64
	RejectedKeys    []string
}

func savedTrackOf(saved catalogbridge.SavedTrack) SavedTrack {
	var evidence struct {
		ISRCSource string `json:"isrc_source"`
	}
	_ = json.Unmarshal(saved.Evidence, &evidence)
	return SavedTrack{
		ISRC:            saved.ISRC,
		ISRCSource:      evidence.ISRCSource,
		Provenance:      saved.Provenance,
		DurationSeconds: saved.DurationSeconds,
		RejectedKeys:    saved.RejectedSourceKeys,
	}
}
