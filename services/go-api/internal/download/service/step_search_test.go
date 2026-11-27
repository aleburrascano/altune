package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/sourcekey"
	"altune/go-api/internal/shared/downloadfailure"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type searchSource struct {
	name   acqdomain.SourceName
	found  []ports.AudioCandidate
	err    error
	onFind func()

	mu       sync.Mutex
	requests []ports.FindRequest
}

func (s *searchSource) Name() acqdomain.SourceName { return s.name }

func (s *searchSource) Find(_ context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()
	if s.onFind != nil {
		s.onFind()
	}
	return s.found, s.err
}

func (s *searchSource) Fetch(context.Context, ports.AudioCandidate, string) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{}, errors.New("fetch refused")
}

func (s *searchSource) seen() []ports.FindRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ports.FindRequest(nil), s.requests...)
}

func runPipeline(ctx context.Context, registry *service.SourceRegistry, ac *service.AcquisitionContext) error {
	steps := coreStepsOver(registry, &objectStore{objects: map[string]string{}}, scratchfiles.New())
	return service.RunPipeline(ctx, steps, ac)
}

func failedStep(t *testing.T, err error) *service.StepError {
	t.Helper()
	var stepErr *service.StepError
	if !errors.As(err, &stepErr) {
		t.Fatalf("err = %v, want a step error", err)
	}
	return stepErr
}

func TestSearchStep_CancelledMidSearch_ReportsCancellation(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"source error without ctx cause", errors.New("all sources failed")},
		{"sources swallowed the cancellation", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			source := &searchSource{name: "ytdlp", err: tt.err, onFind: cancel}

			err := runPipeline(ctx, service.NewSourceRegistry(source), &service.AcquisitionContext{Track: service.TrackRef{Title: "Song", Artist: "Artist"}})

			if failedStep(t, err).Step != "search" {
				t.Errorf("failed step = %v, want search", err)
			}
			if got := service.FailureCodeOf(err); got != string(downloadfailure.FailureAcquisitionCancelled) {
				t.Errorf("FailureCodeOf = %q, want %q", got, downloadfailure.FailureAcquisitionCancelled)
			}
		})
	}
}

func TestSearchStep_Execute(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=abcAAAAAAAA",
			downloadtest.WithTitle("Artist - Song Title"),
			downloadtest.WithDuration(200),
			downloadtest.WithChannel("Artist - Topic"),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(1_000_000),
		),
		downloadtest.Candidate(
			"https://youtube.com/watch?v=defAAAAAAAA",
			downloadtest.WithTitle("Artist - Song Title (Lyrics)"),
			downloadtest.WithDuration(201),
			downloadtest.WithChannel("LyricsChannel"),
			downloadtest.WithCategories([]string{"Music"}),
			downloadtest.WithViewCount(500_000),
		),
	}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Song Title", Artist: "Artist"}}

	_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

	if len(ac.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ac.Candidates))
	}
	if ac.Candidates[0].URL != "https://youtube.com/watch?v=abcAAAAAAAA" {
		t.Errorf("candidate[0].URL = %q, want %q", ac.Candidates[0].URL, "https://youtube.com/watch?v=abcAAAAAAAA")
	}
	if ac.Candidates[1].URL != "https://youtube.com/watch?v=defAAAAAAAA" {
		t.Errorf("candidate[1].URL = %q, want %q", ac.Candidates[1].URL, "https://youtube.com/watch?v=defAAAAAAAA")
	}
}

func TestSearchStep_Execute_NoCandidates(t *testing.T) {
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Nonexistent Song", Artist: "Nobody"}}

	err := runPipeline(context.Background(), service.NewSourceRegistry(&searchSource{name: "ytdlp", found: []ports.AudioCandidate{}}), ac)

	step := failedStep(t, err)
	if step.Step != "search" || step.Err.Error() != "no candidates found" {
		t.Errorf("err = %v, want search failing with %q", err, "no candidates found")
	}
}

func TestSearchStep_Execute_SearchError_NoCandidates(t *testing.T) {
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Some Song", Artist: "Some Artist"}}

	err := runPipeline(context.Background(), service.NewSourceRegistry(&searchSource{name: "ytdlp", err: errors.New("network timeout")}), ac)

	if failedStep(t, err).Step != "search" {
		t.Fatalf("err = %v, want the search step to fail", err)
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("error = %q, want the underlying source failure preserved for the log", err)
	}
	if got := service.FailureCodeOf(err); got != "no_match_found" {
		t.Errorf("client-facing reason = %q, want %q", got, "no_match_found")
	}
}

func TestSearchStep_Execute_DeduplicatesByURL(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{
		downloadtest.Candidate(
			"https://youtube.com/watch?v=sameSameSam",
			downloadtest.WithTitle("Artist - Song"),
			downloadtest.WithDuration(200),
			downloadtest.WithChannel("ArtistVEVO"),
			downloadtest.WithCategories([]string{"Music"}),
		),
	}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Song", Artist: "Artist", Album: "Album", ISRC: "US1234567890"}}

	_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

	if len(ac.Candidates) != 1 {
		t.Errorf("expected 1 deduplicated candidate, got %d", len(ac.Candidates))
	}
}

func TestSearchStep_Name(t *testing.T) {
	if got := service.NewSearchStep(nil).Name(); got != "search" {
		t.Errorf("Name() = %q, want %q", got, "search")
	}
}

func searchWithAllExcluded(t *testing.T, down *searchSource) error {
	t.Helper()
	const url = "https://example.com/a"
	healthy := &searchSource{name: "healthy", found: []ports.AudioCandidate{downloadtest.Candidate(url)}}
	ac := &service.AcquisitionContext{Replace: service.ReplaceState{ExcludeKeys: []string{sourcekey.SourceKey(url)}}}
	err := runPipeline(context.Background(), service.NewSourceRegistry(down, healthy), ac)
	if failedStep(t, err).Step != "search" {
		t.Fatalf("err = %v, want the search step to fail", err)
	}
	return err
}

func throttled() *searchSource {
	return &searchSource{name: "down", err: &acqdomain.SourceUnavailableError{Source: "down", Err: errors.New("429")}}
}

func TestSearchStep_OutageWithAllExcluded_IsTransient(t *testing.T) {
	err := searchWithAllExcluded(t, throttled())

	if !acqdomain.IsSourceUnavailable(err) || service.FailureCodeOf(err) != string(downloadfailure.FailureSourceUnavailable) {
		t.Fatalf("err = %v, want a transient source-unavailable failure", err)
	}
}

func TestSearchStep_PlainSourceErrorWithAllExcluded_StaysPermanent(t *testing.T) {
	err := searchWithAllExcluded(t, &searchSource{name: "down", err: errors.New("exit 1")})

	if acqdomain.IsSourceUnavailable(err) || service.FailureCodeOf(err) != string(downloadfailure.FailureNoMatchFound) {
		t.Fatalf("err = %v, want a permanent no-match failure", err)
	}
}

func TestSearchStep_HealthyAllExcluded_StaysNoMatchFound(t *testing.T) {
	err := searchWithAllExcluded(t, &searchSource{name: "quiet"})

	if got := failedStep(t, err).Err.Error(); got != "no candidates found" {
		t.Errorf("error = %q", got)
	}
	if got := service.FailureCodeOf(err); got != "no_match_found" || acqdomain.IsSourceUnavailable(err) {
		t.Errorf("failure code = %q, want permanent no_match_found", got)
	}
}

func searchedWith(t *testing.T, ac *service.AcquisitionContext) ports.FindRequest {
	t.Helper()
	source := &searchSource{name: "ytdlp"}
	_ = runPipeline(context.Background(), service.NewSourceRegistry(source), ac)
	seen := source.seen()
	if len(seen) == 0 {
		t.Fatal("the source was never searched")
	}
	return seen[0]
}

func TestSearchStep_TheRequestCarriesTheCatalogPins(t *testing.T) {
	explicit := false
	sources := []ports.CatalogSource{{Provider: "ytmusic", ExternalID: "dQw4w9WgXcQ"}}

	req := searchedWith(t, &service.AcquisitionContext{Track: service.TrackRef{CatalogSources: sources, CatalogExplicit: &explicit}})

	if len(req.CatalogSources) != 1 || req.CatalogSources[0] != sources[0] {
		t.Errorf("CatalogSources = %v, want %v", req.CatalogSources, sources)
	}
	if req.CatalogExplicit == nil || *req.CatalogExplicit {
		t.Errorf("CatalogExplicit = %v, want false", req.CatalogExplicit)
	}
	if bare := searchedWith(t, &service.AcquisitionContext{}); bare.CatalogSources != nil || bare.CatalogExplicit != nil {
		t.Errorf("unpinned track yields pins: %+v", bare)
	}
}

func TestSearchStep_TheRequestHintsTheResolvedISRCWhenTheTrackHasNone(t *testing.T) {
	guessed := &service.AcquisitionContext{Identity: ports.RecordingIdentity{ISRC: "CAA509814003"}}
	owned := &service.AcquisitionContext{
		Track:    service.TrackRef{ISRC: "USWB19901214"},
		Identity: ports.RecordingIdentity{ISRC: "DEUM71724939"},
	}

	if got := searchedWith(t, guessed).ISRC; got != "CAA509814003" {
		t.Errorf("hint = %q, want the resolved ISRC", got)
	}
	if got := searchedWith(t, owned).ISRC; got != "USWB19901214" {
		t.Errorf("hint = %q, want the track's own ISRC over the resolver's", got)
	}
}

func TestSearchStep_AFallbackOnlyPassStaysAFallbackOnlyPass(t *testing.T) {
	first := &searchSource{name: "first", found: []ports.AudioCandidate{downloadtest.Candidate("u1", downloadtest.WithTitle("Blinding Lights"))}}
	fallback := &searchSource{name: "fallback", found: []ports.AudioCandidate{downloadtest.Candidate("u2", downloadtest.WithTitle("Blinding Lights"))}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Song Title", Artist: "Artist"}, Pass: service.PassFallbackOnly}

	_ = runPipeline(context.Background(), service.NewSourceRegistryWithFallback([]ports.AudioSource{first}, []ports.AudioSource{fallback}), ac)

	if got := len(first.seen()); got != 0 {
		t.Errorf("first tier finds = %d, want 0: a fallback-only pass skips the first tier", got)
	}
	if ac.Pass != service.PassFallbackOnly {
		t.Errorf("Pass = %v, want the fallback-only pass kept after its search", ac.Pass)
	}
}

func TestSearchStep_AResolvedFallbackHitDoesNotMarkTheFallbackSkipped(t *testing.T) {
	first := &searchSource{name: "first", found: []ports.AudioCandidate{downloadtest.Candidate("u1", downloadtest.WithTitle("Blinding Lights"))}}
	fallback := &searchSource{name: "fallback", found: []ports.AudioCandidate{downloadtest.Candidate(
		"u2",
		downloadtest.WithTitle("Blinding Lights"),
		downloadtest.WithKind(ports.KindArtTrack),
	)}}
	ac := &service.AcquisitionContext{Track: service.TrackRef{Title: "Song Title", Artist: "Artist"}}

	err := runPipeline(context.Background(), service.NewSourceRegistryWithFallback([]ports.AudioSource{first}, []ports.AudioSource{fallback}), ac)

	if step := failedStep(t, err).Step; step == "search" {
		t.Fatalf("failed step = %v, want the search to succeed and a later step to stop the run", err)
	}
	if got := len(fallback.seen()); got != 1 {
		t.Fatalf("fallback finds = %d, want 1: the fallback tier ran", got)
	}
	if ac.Pass == service.PassFallbackSkipped {
		t.Error("Pass = fallback skipped, want not: the fallback ran, only its candidate is resolved")
	}
}
