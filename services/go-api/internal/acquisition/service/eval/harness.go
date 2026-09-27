package eval

import (
	"context"

	"altune/go-api/internal/acquisition/adapters/discoverybridge"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
)

const evalUserID = "eval-user"

type Outcome struct {
	Case      Case
	TopRanked string
	Stored    string
	Failed    bool
	Err       string
	Pass      bool
	Reason    string
	Pending   bool
}

func Run(ctx context.Context, kase Case) Outcome {
	p := newCasePorts(kase)
	ac := &service.AcquisitionContext{
		Track: trackRefFor(kase),
		Replace: service.ReplaceState{
			ExcludeKeys:   service.SourceKeys(kase.ExcludeURLs),
			SkipTopRanked: kase.SkipTopRanked,
		},
	}
	resolveIdentity(ctx, kase, p, ac)

	steps := service.CoreSteps(service.NewSourceRegistry(p), nil, p, p, p)
	runErr := service.RunPipeline(ctx, steps, ac)
	service.CleanupTemp(ctx, ac)
	return outcomeOf(kase, ac, runErr)
}

func resolveIdentity(ctx context.Context, kase Case, p *casePorts, ac *service.AcquisitionContext) {
	if kase.Track.Resolution == nil {
		ac.Identity = identityFor(kase)
		return
	}
	service.ResolveIdentity(ctx, resolverFor(kase), p, ac)
}

func resolverFor(kase Case) *discoverybridge.RecordingResolver {
	res := kase.Track.Resolution
	isrcRecordings := make([]discoverybridge.RecordedISRCRecording, 0, len(res.ISRCRecordings))
	for _, rec := range res.ISRCRecordings {
		isrcRecordings = append(isrcRecordings, discoverybridge.RecordedISRCRecording{MBID: rec.MBID, Duration: rec.Duration})
	}
	return discoverybridge.NewRecordedResolver(searchHitFor(kase), isrcRecordings)
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

func outcomeOf(kase Case, ac *service.AcquisitionContext, runErr error) Outcome {
	out := Outcome{Case: kase, Failed: runErr != nil, Pending: kase.isPending()}
	if runErr != nil {
		out.Err = runErr.Error()
	}
	if len(ac.Ranked) > 0 {
		out.TopRanked = ac.Ranked[0].URL
	}
	if runErr == nil && ac.Selected != nil {
		out.Stored = ac.Selected.URL
	}
	out.Pass, out.Reason = judge(kase, out)
	return out
}

func RunAll(ctx context.Context, cases []Case) []Outcome {
	outcomes := make([]Outcome, 0, len(cases))
	for _, kase := range cases {
		outcomes = append(outcomes, Run(ctx, kase))
	}
	return outcomes
}

func judge(kase Case, out Outcome) (bool, string) {
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
		ISRC:      kase.Track.ISRC,
		MBID:      kase.Track.MBID,
		Duration:  kase.Track.AuthoritativeDuration,
		AcoustIDs: kase.Track.AcoustIDs,
	}
}

func trackRefFor(kase Case) service.TrackRef {
	return service.TrackRef{
		ID:       kase.ID,
		UserID:   evalUserID,
		Title:    kase.Track.Title,
		Artist:   kase.Track.Artist,
		Album:    kase.Track.Album,
		Duration: kase.Track.Duration,
		ISRC:     kase.Track.ISRC,
	}
}
