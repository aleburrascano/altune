package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"strings"
	"testing"
)

type stubSource struct {
	name       acqdomain.SourceName
	found      []ports.AudioCandidate
	findErr    error
	panicValue any
	fetchPath  string
	fetchErr   error
	fetched    []ports.AudioCandidate
	finds      int
}

func (s *stubSource) Name() acqdomain.SourceName { return s.name }

func (s *stubSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.finds++
	if s.panicValue != nil {
		panic(s.panicValue)
	}
	return s.found, s.findErr
}

func (s *stubSource) Fetch(_ context.Context, candidate ports.AudioCandidate, _ string) (ports.FetchedAudio, error) {
	s.fetched = append(s.fetched, candidate)
	return fetchedAt(s.fetchPath, s.fetchErr)
}

func candidate(url string) ports.AudioCandidate {
	return ports.AudioCandidate{Title: "Blinding Lights", URL: url}
}

func resolvedCandidate(url string) ports.AudioCandidate {
	c := candidate(url)
	c.Kind = ports.KindArtTrack
	return c
}

func TestSourceRegistry_NewFiltersNilSources(t *testing.T) {
	reg := NewSourceRegistry(nil, &stubSource{name: "a", found: []ports.AudioCandidate{candidate("u1")}}, nil)
	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("Find returned error: %v, want nil sources dropped", err)
	}
	if len(got) != 1 || got[0].URL != "u1" || got[0].Source != "a" {
		t.Fatalf("Find = %v, want exactly one candidate u1 from source a with nil sources dropped", got)
	}
}

func TestSourceRegistry_FindNoSourcesConfigured(t *testing.T) {
	reg := NewSourceRegistry()
	_, err := reg.Find(context.Background(), ports.FindRequest{})
	if !errors.Is(err, ErrNoAudioSources) {
		t.Fatalf("err = %v, want ErrNoAudioSources", err)
	}
}

func TestSourceRegistry_FindMergesStampsAndDedupesByURL(t *testing.T) {
	a := &stubSource{name: "a", found: []ports.AudioCandidate{candidate("u1"), candidate("shared")}}
	b := &stubSource{name: "b", found: []ports.AudioCandidate{candidate("shared"), candidate("u2"), candidate("")}}
	reg := NewSourceRegistry(a, b)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}

	wantURLs := []string{"u1", "shared", "u2"}
	if len(got) != len(wantURLs) {
		t.Fatalf("got %d candidates %v, want %v", len(got), got, wantURLs)
	}
	for i, c := range got {
		if c.URL != wantURLs[i] {
			t.Errorf("candidate %d URL = %q, want %q", i, c.URL, wantURLs[i])
		}
	}
	if got[0].Source != "a" || got[1].Source != "a" || got[2].Source != "b" {
		t.Errorf("sources = %q/%q/%q, want a/a/b — first source wins a duplicate URL", got[0].Source, got[1].Source, got[2].Source)
	}
}

func TestSourceRegistry_FindSurvivesPanickingSource(t *testing.T) {
	panicky := &stubSource{name: "panicky", panicValue: "boom"}
	healthy := &stubSource{name: "healthy", found: []ports.AudioCandidate{candidate("u1")}}
	reg := NewSourceRegistry(panicky, healthy)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("one panicking source must not fail the search, got err: %v", err)
	}
	if len(got) != 1 || got[0].URL != "u1" || got[0].Source != "healthy" {
		t.Fatalf("got %v, want the healthy source's single candidate", got)
	}
}

func TestSourceRegistry_FindReturnsPartialOnSingleFailure(t *testing.T) {
	failing := &stubSource{name: "failing", findErr: errors.New("down")}
	healthy := &stubSource{name: "healthy", found: []ports.AudioCandidate{candidate("u1")}}
	reg := NewSourceRegistry(failing, healthy)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("one failing source must not fail the search, got err: %v", err)
	}
	if len(got) != 1 || got[0].URL != "u1" {
		t.Fatalf("got %v, want only the healthy source's candidate", got)
	}
}

func TestSourceRegistry_FindFailsWhenEverySourceFails(t *testing.T) {
	first := &stubSource{name: "first", findErr: errors.New("first down")}
	second := &stubSource{name: "second", panicValue: "second boom"}
	reg := NewSourceRegistry(first, second)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if got != nil {
		t.Fatalf("got %v, want nil when every source failed", got)
	}
	if err == nil || !strings.Contains(err.Error(), "every audio source failed") {
		t.Fatalf("err = %v, want it to report every audio source failed", err)
	}
	if !strings.Contains(err.Error(), "first down") {
		t.Errorf("err = %v, want it to wrap the first failure", err)
	}
}

func TestMergeSlots_DedupesFailsAllAndKeepsFirstError(t *testing.T) {
	sources := []ports.AudioSource{
		&stubSource{name: "a"},
		&stubSource{name: "b"},
		&stubSource{name: "c"},
	}
	slots := [][]ports.AudioCandidate{
		{candidate("u1"), candidate("dup")},
		nil,
		{candidate("dup"), candidate("u2")},
	}
	errs := []error{nil, errors.New("b failed"), nil}

	got, err := mergeSlots(context.Background(), sources, slots, errs)
	if err != nil {
		t.Fatalf("mergeSlots returned error with a surviving source: %v", err)
	}
	wantURLs := []string{"u1", "dup", "u2"}
	if len(got) != len(wantURLs) {
		t.Fatalf("got %v, want %v", got, wantURLs)
	}
	for i, c := range got {
		if c.URL != wantURLs[i] {
			t.Errorf("candidate %d URL = %q, want %q", i, c.URL, wantURLs[i])
		}
	}

	allErrs := []error{errors.New("a failed"), errors.New("b failed"), errors.New("c failed")}
	_, err = mergeSlots(context.Background(), sources, [][]ports.AudioCandidate{nil, nil, nil}, allErrs)
	if err == nil || !strings.Contains(err.Error(), "a failed") {
		t.Fatalf("err = %v, want every-source-failed wrapping the first error", err)
	}
}

func TestMergeSlots_CollapsesOneVideoSpelledByTwoSources(t *testing.T) {
	sources := []ports.AudioSource{&stubSource{name: "ytmusic"}, &stubSource{name: "ytdlp"}}
	slots := [][]ports.AudioCandidate{
		{candidate("https://music.youtube.com/watch?v=dQw4w9WgXcQ")},
		{candidate("https://www.youtube.com/watch?v=dQw4w9WgXcQ&list=RDabc")},
	}

	got, err := mergeSlots(context.Background(), sources, slots, []error{nil, nil})
	if err != nil {
		t.Fatalf("mergeSlots returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d candidates %v, want 1 — one video must not spend two download attempts", len(got), got)
	}
}

func TestMergeSlots_PrefersTheResolvedSpellingOfOneVideo(t *testing.T) {
	sources := []ports.AudioSource{&stubSource{name: "ytdlp"}, &stubSource{name: "ytmusic"}}
	slots := [][]ports.AudioCandidate{
		{candidate("https://www.youtube.com/watch?v=dQw4w9WgXcQ")},
		{resolvedCandidate("https://music.youtube.com/watch?v=dQw4w9WgXcQ")},
	}

	got, err := mergeSlots(context.Background(), sources, slots, []error{nil, nil})
	if err != nil {
		t.Fatalf("mergeSlots returned error: %v", err)
	}
	if len(got) != 1 || !got[0].IsResolved() {
		t.Fatalf("got %v, want the one resolved candidate — dedupe must not drop a resolution", got)
	}
}

func TestMergeSlots_KeepsTheResolvedCandidateAheadOfALaterDuplicate(t *testing.T) {
	sources := []ports.AudioSource{&stubSource{name: "ytmusic"}, &stubSource{name: "ytdlp"}}
	slots := [][]ports.AudioCandidate{
		{resolvedCandidate("https://music.youtube.com/watch?v=dQw4w9WgXcQ")},
		{candidate("https://www.youtube.com/watch?v=dQw4w9WgXcQ")},
	}

	got, err := mergeSlots(context.Background(), sources, slots, []error{nil, nil})
	if err != nil {
		t.Fatalf("mergeSlots returned error: %v", err)
	}
	if len(got) != 1 || !got[0].IsResolved() {
		t.Fatalf("got %v, want the one resolved candidate — an unresolved duplicate must not displace it", got)
	}
}

func TestSourceRegistry_FetchRoutesToOwningSource(t *testing.T) {
	a := &stubSource{name: "a"}
	b := &stubSource{name: "b", fetchPath: "/tmp/b.mp3"}
	reg := NewSourceRegistry(a, b)

	fetched, err := reg.Fetch(context.Background(), ports.AudioCandidate{URL: "u2", Source: "b"}, "/out")
	if err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if fetched.Path != "/tmp/b.mp3" {
		t.Errorf("path = %q, want the owning source's result", fetched.Path)
	}
	if len(a.fetched) != 0 {
		t.Errorf("source a received %d fetches, want 0", len(a.fetched))
	}
	if len(b.fetched) != 1 || b.fetched[0].URL != "u2" {
		t.Errorf("source b fetched = %v, want the u2 candidate", b.fetched)
	}
}

func TestSourceRegistry_FetchUnknownSource(t *testing.T) {
	reg := NewSourceRegistry(&stubSource{name: "a"})

	_, err := reg.Fetch(context.Background(), ports.AudioCandidate{URL: "u1", Source: "ghost"}, "/out")
	if err == nil {
		t.Fatal("expected an error routing to an unknown source")
	}
	if !strings.Contains(err.Error(), "ghost") || !strings.Contains(err.Error(), "u1") {
		t.Errorf("err = %v, want it to name the missing source and candidate", err)
	}
}

func throttledSource() *stubSource {
	return &stubSource{
		name:    "ytdlp",
		findErr: &acqdomain.SourceUnavailableError{Source: "ytdlp", Err: errors.New("HTTP Error 429")},
	}
}

func searchFailureCode(t *testing.T, reg *SourceRegistry) domain.FailureCode {
	t.Helper()
	_, err := NewSearchStep(reg).Execute(context.Background(), &AcquisitionContext{}, pipelineStart{})
	if err == nil {
		t.Fatal("search step succeeded, want a failure")
	}
	return failureCode(&StepError{Step: stepNameSearch, Err: err})
}

func TestSourceRegistry_FindEmptyWithUnavailableSourceReportsUnavailable(t *testing.T) {
	reg := NewSourceRegistry(throttledSource(), &stubSource{name: "ytmusic"})

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if !acqdomain.IsSourceUnavailable(err) || len(got) != 0 {
		t.Fatalf("Find = %v, %v; want empty and a source-unavailable error", got, err)
	}
	if code := searchFailureCode(t, reg); code != domain.FailureSourceUnavailable {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureSourceUnavailable)
	}
}

func TestSourceRegistry_FindPartialFailureWithCandidatesSucceeds(t *testing.T) {
	reg := NewSourceRegistry(
		throttledSource(),
		&stubSource{name: "ytmusic", found: []ports.AudioCandidate{candidate("u1")}},
	)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %v, %v; want one candidate and nil error", got, err)
	}
}

func TestSourceRegistry_FindAllEmptyWithoutErrorIsNoMatch(t *testing.T) {
	reg := NewSourceRegistry(&stubSource{name: "a"}, &stubSource{name: "b"})

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil || len(got) != 0 {
		t.Fatalf("Find = %v, %v; want empty and nil error", got, err)
	}
	if code := searchFailureCode(t, reg); code != domain.FailureNoMatchFound {
		t.Fatalf("failure code = %s, want %s", code, domain.FailureNoMatchFound)
	}
}

func mergeSlots(
	ctx context.Context,
	sources []ports.AudioSource,
	slots [][]ports.AudioCandidate,
	errs []error,
) ([]ports.AudioCandidate, error) {
	res, err := mergeSlotsReportingOutage(ctx, sources, slots, errs)
	return res.Candidates, err
}

func fetchedAt(path string, err error) (ports.FetchedAudio, error) {
	return ports.FetchedAudio{Path: path}, err
}

func TestSourceRegistry_AResolvedFirstTierHitSkipsTheFallback(t *testing.T) {
	first := &stubSource{name: "ytmsearch", found: []ports.AudioCandidate{resolvedCandidate("https://music.youtube.com/watch?v=abc")}}
	fallback := &stubSource{name: "ytdlp", found: []ports.AudioCandidate{candidate("u1")}}
	reg := NewSourceRegistry(first).WithFallback(fallback)

	got, err := reg.Find(context.Background(), ports.FindRequest{})
	if err != nil {
		t.Fatalf("Find returned error: %v", err)
	}
	if fallback.finds != 0 {
		t.Errorf("fallback searched %d times, want 0 after a resolved first-tier hit", fallback.finds)
	}
	if len(got) != 1 || got[0].Source != "ytmsearch" {
		t.Errorf("Find = %v, want only the resolved first-tier candidate", got)
	}
}

func TestSourceRegistry_TheFallbackRunsWhenTheFirstTierHasNoResolvedHit(t *testing.T) {
	for name, first := range map[string]*stubSource{
		"unresolved only": {name: "ytmsearch", found: []ports.AudioCandidate{candidate("u0")}},
		"nothing":         {name: "ytmsearch"},
		"error":           {name: "ytmsearch", findErr: errors.New("innertube 503")},
	} {
		t.Run(name, func(t *testing.T) {
			fallback := &stubSource{name: "ytdlp", found: []ports.AudioCandidate{candidate("u1")}}
			reg := NewSourceRegistry(first).WithFallback(fallback)

			got, err := reg.Find(context.Background(), ports.FindRequest{})
			if err != nil {
				t.Fatalf("Find returned error: %v", err)
			}
			if fallback.finds != 1 {
				t.Errorf("fallback searched %d times, want 1", fallback.finds)
			}
			if len(got) == 0 || got[len(got)-1].URL != "u1" || got[len(got)-1].Source != "ytdlp" {
				t.Errorf("Find = %v, want the fallback's candidate merged in", got)
			}
		})
	}
}

func TestSourceRegistry_FetchReachesFallbackSources(t *testing.T) {
	fallback := &stubSource{name: "ytdlp", fetchPath: "/tmp/a.m4a"}
	reg := NewSourceRegistry(&stubSource{name: "ytmsearch"}).WithFallback(fallback)

	if _, err := reg.Fetch(context.Background(), ports.AudioCandidate{Source: "ytdlp", URL: "u1"}, t.TempDir()); err != nil {
		t.Fatalf("Fetch returned error: %v", err)
	}
	if len(fallback.fetched) != 1 {
		t.Errorf("fallback fetched %d times, want 1", len(fallback.fetched))
	}
}

func TestSourceRegistry_TheKindDecidesTheFallback(t *testing.T) {
	artTrack := candidate("https://music.youtube.com/watch?v=abc")
	artTrack.Kind = ports.KindArtTrack
	upload := candidate("https://www.youtube.com/watch?v=def")
	upload.Kind = ports.KindUpload

	for name, tc := range map[string]struct {
		hit            ports.AudioCandidate
		wantSecondTier int
	}{
		"art track": {artTrack, 0},
		"upload":    {upload, 1},
	} {
		t.Run(name, func(t *testing.T) {
			first := &stubSource{name: "ytmsearch", found: []ports.AudioCandidate{tc.hit}}
			fallback := &stubSource{name: "ytdlp", found: []ports.AudioCandidate{candidate("u1")}}
			reg := NewSourceRegistry(first).WithFallback(fallback)

			if _, err := reg.Find(context.Background(), ports.FindRequest{}); err != nil {
				t.Fatalf("Find returned error: %v", err)
			}
			if fallback.finds != tc.wantSecondTier {
				t.Errorf("fallback searched %d times, want %d", fallback.finds, tc.wantSecondTier)
			}
		})
	}
}

func TestSourceRegistry_ReportsWhetherItSkippedTheFallback(t *testing.T) {
	tests := []struct {
		name         string
		first        []ports.AudioCandidate
		withFallback bool
		req          ports.FindRequest
		want         bool
	}{
		{"resolved first-tier hit skips the configured fallback", []ports.AudioCandidate{resolvedCandidate("u1")}, true, ports.FindRequest{}, true},
		{"unresolved first tier runs the fallback", []ports.AudioCandidate{candidate("u1")}, true, ports.FindRequest{}, false},
		{"no fallback configured", []ports.AudioCandidate{resolvedCandidate("u1")}, false, ports.FindRequest{}, false},
		{"fallback only", []ports.AudioCandidate{resolvedCandidate("u1")}, true, ports.FindRequest{FallbackOnly: true}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := NewSourceRegistry(&stubSource{name: "first", found: tt.first})
			if tt.withFallback {
				reg.WithFallback(&stubSource{name: "fb", found: []ports.AudioCandidate{candidate("u2")}})
			}
			res, err := reg.FindReportingOutage(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("FindReportingOutage: %v", err)
			}
			if res.FallbackSkipped != tt.want {
				t.Errorf("FallbackSkipped = %v, want %v", res.FallbackSkipped, tt.want)
			}
		})
	}
}
