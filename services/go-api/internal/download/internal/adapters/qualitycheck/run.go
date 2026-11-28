package qualitycheck

import (
	"altune/go-api/internal/download/internal/adapters/evidence"
	"altune/go-api/internal/download/internal/adapters/memory"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/songfields"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
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

func Run(ctx context.Context, now clock.Func, newIDs ids.Source, kase Case) Outcome {
	p := newCasePorts(kase)
	track := seededTrack(newIDs, kase)
	ledger := memory.NewLedger(memory.LedgerTrack{TrackSnapshot: track})

	var observed *service.AcquisitionContext
	wiring := service.VerificationPorts{
		Identifier:      p,
		RecordingLookup: recordedLookups(kase.Recordings),
		Rerecordings:    memory.NoopRerecordingFinder(),
		Previews:        memory.NoopPreviewMatcher(),
		Recordings:      resolverFor(kase),
	}
	if kase.Rerecording != nil {
		wiring.Rerecordings = recordedRerecording{*kase.Rerecording}
	}
	stages := service.StagePorts{
		Prober:        p,
		Tagger:        memory.NoopAudioTagger(),
		Rejections:    memory.NoopRejectionStore(),
		StoredAudio:   p,
		Orphans:       service.MustDirectDeleteOrphans(p),
		Skips:         memory.NoopVerifySkipRecorder(),
		Evidence:      evidence.NewCodec(),
		Events:        events.NoopPublisher(),
		DownloadLimit: service.Unlimited(),
		Observer:      func(ac *service.AcquisitionContext) { observed = ac },
		Sleep:         clock.Sleep,
		Now:           now,
		Unit:          random.Real,
	}
	svc := service.MustNewAcquireTrackAudioService(ledger, p.registry(), p, scratchfiles.New(), wiring, stages)
	runErr := executeFor(ctx, svc, kase, track)
	if observed == nil && runErr == nil {
		runErr = errors.New("eval: the service never started an acquisition")
	}
	return outcomeOf(kase, observed, runErr, p.clock, savedTrackOf(ledger, track.ID))
}

type recordedRerecording struct{ answer RecordedRerecording }

func (r recordedRerecording) FindRerecording(_ context.Context, q domain.RerecordingQuery) (domain.TrackLabel, bool, error) {
	if q.OriginalMBID != r.answer.OriginalMBID {
		return domain.TrackLabel{}, false, nil
	}
	return domain.TrackLabel{
		MBID:            r.answer.MBID,
		Title:           r.answer.Title,
		Album:           r.answer.Album,
		Year:            r.answer.Year,
		ISRC:            r.answer.ISRC,
		DurationSeconds: r.answer.Duration,
	}, true, nil
}

func executeFor(ctx context.Context, svc *service.AcquireTrackAudioService, kase Case, track domain.TrackSnapshot) error {
	if kase.Stored != nil {
		if err := service.NewReplaceService(svc).ExecuteReplace(ctx, domain.AcquisitionJob{UserID: track.UserId, TrackID: track.ID}); err != nil {
			return fmt.Errorf("execute replace: %w", err)
		}
		return nil
	}
	if err := svc.Execute(ctx, domain.AcquisitionJob{UserID: track.UserId, TrackID: track.ID}); err != nil {
		return fmt.Errorf("execute acquisition: %w", err)
	}
	return nil
}

func seededTrack(newIDs ids.Source, kase Case) domain.TrackSnapshot {
	t := kase.Track
	track := domain.TrackSnapshot{
		ID:                 newIDs.NewTrackId(),
		UserId:             newIDs.NewUserId(),
		Title:              strings.TrimSpace(t.Title),
		Artist:             catalogField(t.Artist),
		Album:              t.Album,
		ISRC:               optional(t.ISRC),
		CatalogMBID:        optional(t.CatalogMBID),
		CatalogExplicit:    t.CatalogExplicit,
		CatalogSources:     t.portCatalogSources(),
		AudioSourceURL:     optional(t.AudioSourceURL),
		RejectedSourceKeys: rejectedKeys(kase.ExcludeURLs),
		Status:             domain.TrackPending,
	}
	if length := targetLength(t); length > 0 {
		track.CatalogDurationSeconds, track.StoredDurationSeconds = &length, &length
	}
	if kase.Stored == nil {
		return track
	}
	ref := storedAudioRef
	track.Status, track.AudioRef, track.Streamable = domain.TrackReady, &ref, true
	if length := kase.Stored.Duration; length > 0 {
		track.StoredDurationSeconds = &length
	}
	return track
}

func catalogField(s string) string {
	return strings.Join(strings.Fields(textnorm.NFKC(s)), " ")
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
		key := domain.SourceKey(rawURL)
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
			keys = keys[max(0, len(keys)-maxRejectedSourceKeys):]
		}
	}
	return keys
}

func resolverFor(kase Case) ports.RecordingResolver {
	res := kase.Track.Resolution
	if res == nil {
		return copiedIdentity(identityFor(kase))
	}
	isrcRecordings := make([]domain.ISRCRecording, 0, len(res.ISRCRecordings))
	for _, rec := range res.ISRCRecordings {
		isrcRecordings = append(isrcRecordings, domain.ISRCRecording{MBID: rec.MBID, Duration: float64(rec.Duration)})
	}
	return service.NewRecordedResolver(searchHitsFor(res), isrcRecordings, pinnedFactsFor(res))
}

func pinnedFactsFor(res *Resolution) *domain.RecordingFacts {
	if res.Pinned == nil {
		return nil
	}
	facts := res.Pinned.facts()
	return &facts
}

func searchHitsFor(res *Resolution) []songfields.RecordingIdentity {
	hits := make([]songfields.RecordingIdentity, 0, len(res.Search))
	for _, hit := range res.Search {
		hits = append(hits, songfields.RecordingIdentity{
			Title:           hit.Title,
			Artist:          hit.Artist,
			Album:           hit.Album,
			ISRC:            hit.ISRC,
			RecordingMBID:   hit.MBID,
			DurationSeconds: float64(hit.Duration),
		})
	}
	return hits
}

func outcomeOf(kase Case, ac *service.AcquisitionContext, runErr error, sim *simClock, saved SavedTrack) Outcome {
	if ac == nil {
		ac = &service.AcquisitionContext{}
	}
	out := Outcome{
		Case:             kase,
		Failed:           runErr != nil,
		Pending:          kase.isPending(),
		SimulatedSeconds: sim.searchSeconds() + downloadSeconds(kase, ac.Attempted),
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

func RunAll(ctx context.Context, now clock.Func, newIDs ids.Source, cases []Case) []Outcome {
	outcomes := make([]Outcome, 0, len(cases))
	for _, kase := range cases {
		outcomes = append(outcomes, Run(ctx, now, newIDs, kase))
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
	if !kase.hasCorrectCandidate() && out.Failed {
		return true, "correctly acquired nothing"
	}
	if !kase.hasCorrectCandidate() {
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

func identityFor(kase Case) domain.RecordingIdentity {
	return domain.RecordingIdentity{
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

func (l recordedLookups) RecordingByMBID(_ context.Context, mbid string) (domain.RecordingFacts, bool, error) {
	for _, rec := range l {
		if rec.MBID == mbid {
			return rec.facts(), true, nil
		}
	}
	return domain.RecordingFacts{}, false, nil
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

const unreadableEvidence = "unreadable-evidence"

func savedTrackOf(ledger *memory.Ledger, id ids.TrackId) SavedTrack {
	saved, ok := ledger.Track(id)
	if !ok {
		return SavedTrack{}
	}
	var stored struct {
		ISRCSource string `json:"isrc_source"`
	}
	if len(saved.Evidence) > 0 && json.Unmarshal(saved.Evidence, &stored) != nil {
		stored.ISRCSource = unreadableEvidence
	}
	out := SavedTrack{
		ISRCSource:   stored.ISRCSource,
		Provenance:   string(saved.Provenance),
		RejectedKeys: saved.RejectedSourceKeys,
		Title:        saved.Title,
		Album:        saved.Album,
	}
	if saved.ISRC != nil {
		out.ISRC = *saved.ISRC
	}
	if saved.StoredDurationSeconds != nil {
		out.DurationSeconds = *saved.StoredDurationSeconds
	}
	if saved.Year != nil {
		out.Year = *saved.Year
	}
	if saved.CatalogMBID != nil {
		out.CatalogMBID = *saved.CatalogMBID
	}
	return out
}
