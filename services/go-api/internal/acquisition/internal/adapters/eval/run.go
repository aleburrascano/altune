package eval

import (
	"altune/go-api/internal/acquisition/internal/adapters/acquisitiontest"
	"altune/go-api/internal/acquisition/internal/adapters/discoverybridge"
	"altune/go-api/internal/acquisition/internal/adapters/tempfs"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/sourcekey"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"
)

const (
	storedAudioRef        = "eval/stored.mp3"
	maxRejectedSourceKeys = 25
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
	track := seededTrack(kase)
	ledger := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: track})

	var observed *service.AcquisitionContext
	opts := []func(*service.AcquireTrackAudioService){
		service.WithAudioProber(p),
		service.WithAudioIdentifier(p),
		service.WithRecordingResolver(resolverFor(kase)),
		service.WithAcquireRecordingLookup(recordedLookups(kase.Recordings)),
		service.WithAcquireTempWorkspace(tempfs.New()),
		service.WithStoredAudioReader(p),
		service.WithContextObserver(func(ac *service.AcquisitionContext) { observed = ac }),
	}
	if kase.Rerecording != nil {
		opts = append(opts, service.WithRerecordingFinder(recordedRerecording{*kase.Rerecording}))
	}
	svc := service.NewAcquireTrackAudioService(ledger, p.registry(), p, opts...)
	runErr := executeFor(ctx, svc, kase, track)
	if observed == nil && runErr == nil {
		runErr = errors.New("eval: the service never started an acquisition")
	}
	return outcomeOf(kase, observed, runErr, p.clock, savedTrackOf(ledger, track.ID))
}

type recordedRerecording struct{ answer RecordedRerecording }

func (r recordedRerecording) FindRerecording(_ context.Context, q ports.RerecordingQuery) (ports.RecordingLabel, bool, error) {
	if q.OriginalMBID != r.answer.OriginalMBID {
		return ports.RecordingLabel{}, false, nil
	}
	return ports.RecordingLabel{
		MBID:            r.answer.MBID,
		Title:           r.answer.Title,
		Album:           r.answer.Album,
		Year:            r.answer.Year,
		ISRC:            r.answer.ISRC,
		DurationSeconds: r.answer.Duration,
	}, true, nil
}

func executeFor(ctx context.Context, svc *service.AcquireTrackAudioService, kase Case, track ports.TrackSnapshot) error {
	if kase.Stored != nil {
		return svc.ExecuteReplace(ctx, track.UserId, track.ID)
	}
	return svc.Execute(ctx, track.UserId, track.ID)
}

func seededTrack(kase Case) ports.TrackSnapshot {
	t := kase.Track
	track := ports.TrackSnapshot{
		ID:                 shared.NewTrackId(),
		UserId:             shared.NewUserId(uuid.New()),
		Title:              strings.TrimSpace(t.Title),
		Artist:             strings.Join(strings.Fields(textnorm.NFKC(t.Artist)), " "),
		Album:              t.Album,
		ISRC:               optional(t.ISRC),
		CatalogMBID:        optional(t.CatalogMBID),
		CatalogExplicit:    t.CatalogExplicit,
		CatalogSources:     t.portCatalogSources(),
		AudioSourceURL:     optional(t.AudioSourceURL),
		RejectedSourceKeys: rejectedKeys(kase.ExcludeURLs),
		Status:             ports.TrackPending,
	}
	if length := targetLength(t); length > 0 {
		track.CatalogDurationSeconds, track.StoredDurationSeconds = &length, &length
	}
	if kase.Stored != nil {
		ref, length := storedAudioRef, kase.Stored.Duration
		track.Status, track.AudioRef, track.StoredDurationSeconds = ports.TrackReady, &ref, &length
	}
	return track
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func rejectedKeys(urls []string) []string {
	var keys []string
	for _, rawURL := range urls {
		key := sourcekey.SourceKey(rawURL)
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys[max(0, len(keys)-maxRejectedSourceKeys):]
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
	return discoverybridge.NewRecordedResolver(searchHitsFor(res), isrcRecordings, pinnedFactsFor(res))
}

func pinnedFactsFor(res *Resolution) *ports.RecordingFacts {
	if res.Pinned == nil {
		return nil
	}
	facts := res.Pinned.facts()
	return &facts
}

func searchHitsFor(res *Resolution) []discoverybridge.RecordedSearchHit {
	hits := make([]discoverybridge.RecordedSearchHit, 0, len(res.Search))
	for _, hit := range res.Search {
		hits = append(hits, discoverybridge.RecordedSearchHit{
			Title:    hit.Title,
			Artist:   hit.Artist,
			Album:    hit.Album,
			ISRC:     hit.ISRC,
			MBID:     hit.MBID,
			Duration: hit.Duration,
		})
	}
	return hits
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
	case want.Title != "" && got.Title != want.Title:
		return "want saved title " + want.Title + ", got " + got.Title, false
	case want.Album != "" && got.Album != want.Album:
		return "want saved album " + want.Album + ", got " + got.Album, false
	case want.Year != 0 && got.Year != want.Year:
		return fmt.Sprintf("want saved year %d, got %d", want.Year, got.Year), false
	case want.MBID != "" && got.CatalogMBID != want.MBID:
		return "want saved mbid " + want.MBID + ", got " + got.CatalogMBID, false
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
	Title           string
	Album           string
	Year            int
	CatalogMBID     string
}

func savedTrackOf(ledger *acquisitiontest.Ledger, id shared.TrackId) SavedTrack {
	saved, ok := ledger.Track(id)
	if !ok {
		return SavedTrack{}
	}
	var evidence struct {
		ISRCSource string `json:"isrc_source"`
	}
	_ = json.Unmarshal(saved.Evidence, &evidence)
	return SavedTrack{
		ISRC:            derefOrZero(saved.ISRC),
		ISRCSource:      evidence.ISRCSource,
		Provenance:      string(saved.Provenance),
		DurationSeconds: derefOrZero(saved.StoredDurationSeconds),
		RejectedKeys:    saved.RejectedSourceKeys,
		Title:           saved.Title,
		Album:           saved.Album,
		Year:            derefOrZero(saved.Year),
		CatalogMBID:     derefOrZero(saved.CatalogMBID),
	}
}

func derefOrZero[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
