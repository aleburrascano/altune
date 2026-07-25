package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func TestArtworkPathFor(t *testing.T) {
	tests := []struct {
		name        string
		resolved    string
		confidence  ports.ArtworkConfidence
		fromDurable bool
		want        string
	}{
		{"nothing resolved", "", ports.ArtworkConfidenceIdentity, true, "none"},
		{"identity via durable store", "u", ports.ArtworkConfidenceIdentity, true, "durable-identity"},
		{"identity from this fan-out", "u", ports.ArtworkConfidenceIdentity, false, "identity"},
		{"provisional name search", "u", ports.ArtworkConfidenceName, false, "name"},
		{"name confidence never masquerades as durable", "u", ports.ArtworkConfidenceName, true, "name"},
		{"no confidence falls back to provider", "u", ports.ArtworkConfidenceNone, false, "provider"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := artworkPathFor(tt.resolved, tt.confidence, tt.fromDurable); got != tt.want {
				t.Errorf("artworkPathFor(%q,%v,%v) = %q, want %q", tt.resolved, tt.confidence, tt.fromDurable, got, tt.want)
			}
		})
	}
}

func TestCircuitBreaker_FullStateWalkUnderConcurrency(t *testing.T) {
	cb := NewCircuitBreaker()
	p := domain.ProviderDeezer

	hammer := func(n int, f func()) {
		var wg sync.WaitGroup
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); f() }()
		}
		wg.Wait()
	}

	hammer(16, func() {
		if !cb.AllowRequest(p) {
			t.Error("closed breaker must allow requests")
		}
	})

	hammer(failureThreshold, func() { cb.RecordFailure(p) })
	if cb.AllowRequest(p) {
		t.Fatal("breaker must be open after the failure threshold")
	}
	if cb.GetStatus(p) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("status = %v, want circuit_open", cb.GetStatus(p))
	}

	cb.mu.Lock()
	cb.circuits[p].lastFailedAt = time.Now().Add(-openDuration - time.Second)
	cb.mu.Unlock()
	var admitted int32
	var mu sync.Mutex
	hammer(16, func() {
		if cb.AllowRequest(p) {
			mu.Lock()
			admitted++
			mu.Unlock()
		}
	})
	if admitted != 1 {
		t.Fatalf("half-open admitted %d probes, want exactly 1", admitted)
	}
	if cb.GetStatus(p) != domain.ProviderStatusOK {
		t.Errorf("half-open status = %v, want ok", cb.GetStatus(p))
	}

	cb.RecordSuccess(p)
	hammer(16, func() {
		if !cb.AllowRequest(p) {
			t.Error("re-closed breaker must allow requests")
		}
	})

	hammer(failureThreshold, func() { cb.RecordFailure(p) })
	if cb.AllowRequest(p) {
		t.Fatal("breaker must re-open after a fresh failure threshold")
	}

	cb.mu.Lock()
	cb.circuits[p].lastFailedAt = time.Now().Add(-openDuration - time.Second)
	cb.mu.Unlock()
	if !cb.AllowRequest(p) {
		t.Fatal("aged-open breaker must admit the probe")
	}
	cb.RecordFailure(p)
	if cb.AllowRequest(p) {
		t.Error("a failed half-open probe must re-open the breaker immediately")
	}
}

func TestCorrection_NilVocabIsNil(t *testing.T) {
	s := NewCorrectionService(nil)
	if s.Correct(context.Background(), "humble") != nil {
		t.Error("Correct with no vocab must be nil")
	}
	if s.CorrectAggressive(context.Background(), "humble") != nil {
		t.Error("CorrectAggressive with no vocab must be nil")
	}
}

func TestCorrection_CorrectWholeQuery(t *testing.T) {
	store := &fakeVocabularyStore{
		findClosestFn: func(query string, _ int) ([]domain.VocabularyEntry, error) {
			return []domain.VocabularyEntry{
				{Term: "Kendrick", TermNorm: "kendrick", Kind: domain.VocabKindArtist, MatchScore: 0.9},
			}, nil
		},
	}
	s := NewCorrectionService(store)

	got := s.Correct(context.Background(), "kendrik")
	if got == nil || got.Corrected != "Kendrick" {
		t.Fatalf("Correct = %+v, want the distance-1 vocab term", got)
	}

	if got := s.Correct(context.Background(), "kendrick"); got != nil {
		t.Errorf("exact vocab term corrected to %+v, want nil", got)
	}
}

func TestCorrection_WholeQueryErrorOrEmptyIsNil(t *testing.T) {
	erroring := &fakeVocabularyStore{
		findClosestFn: func(string, int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
	}
	if got := NewCorrectionService(erroring).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("store error must degrade to nil, got %+v", got)
	}
	empty := &fakeVocabularyStore{}
	if got := NewCorrectionService(empty).Correct(context.Background(), "kendrik"); got != nil {
		t.Errorf("no candidates must yield nil, got %+v", got)
	}
}

func TestMaxCorrectionDist_Boundaries(t *testing.T) {
	tests := []struct {
		query string
		want  int
	}{
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"abcdefghi", 3},
	}
	for _, tt := range tests {
		if got := maxCorrectionDist(tt.query); got != tt.want {
			t.Errorf("maxCorrectionDist(%q) = %d, want %d", tt.query, got, tt.want)
		}
	}
}

func TestCorrection_TokenPathSingleTokenIsNil(t *testing.T) {
	store := &fakeVocabularyStore{}
	s := NewCorrectionService(store)
	if got := s.CorrectAggressive(context.Background(), "kendrik"); got != nil {
		t.Errorf("single-token aggressive miss = %+v, want nil", got)
	}
}

func TestCorrection_TokenPathPrefixErrorDegrades(t *testing.T) {
	store := &fakeVocabularyStore{
		suggestByPrefixFn: func(string, int) ([]domain.VocabularyEntry, error) {
			return nil, errors.New("redis down")
		},
		findClosestFn: func(query string, _ int) ([]domain.VocabularyEntry, error) {
			if query == "kendrik lamar" {
				return nil, nil
			}
			if query == "kendrik" {
				return []domain.VocabularyEntry{{Term: "Kendrick", TermNorm: "kendrick", Kind: domain.VocabKindArtist, MatchScore: 0.8}}, nil
			}
			return []domain.VocabularyEntry{{Term: "Lamar", TermNorm: "lamar", Kind: domain.VocabKindArtist, MatchScore: 1}}, nil
		},
	}
	s := NewCorrectionService(store)
	got := s.CorrectAggressive(context.Background(), "kendrik lamar")
	if got == nil || got.Corrected != "kendrick lamar" {
		t.Fatalf("aggressive = %+v, want token-corrected \"kendrick lamar\"", got)
	}
	if got.Confidence != 0.8 {
		t.Errorf("confidence = %v, want the min corrected-token score 0.8", got.Confidence)
	}
}

func TestRecordEvent_NonClientSubmittableRenders400(t *testing.T) {
	store := &fakeEventStore{}
	svc := NewRecordEventService(store)

	err := svc.Execute(context.Background(), newUser(), RecordEventInput{
		Type: domain.EventTypeSearchPerformed,
	})
	if err == nil {
		t.Fatal("want a validation error")
	}
	var se interface{ HTTPStatus() int }
	if !errors.As(err, &se) || se.HTTPStatus() != 400 {
		t.Fatalf("error = %v, want an HTTP 400 StatusError", err)
	}
	if !strings.Contains(err.Error(), "not client-submittable") {
		t.Errorf("Error() = %q, want the not-client-submittable message", err.Error())
	}
	if len(store.recorded()) != 0 {
		t.Error("rejected event must not be appended")
	}
}

type plainArtistProvider struct{}

func (plainArtistProvider) GetArtistTopTracks(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return nil, nil
}
func (plainArtistProvider) GetArtistAlbums(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
	return nil, nil
}

func TestResolveArtistIDByName(t *testing.T) {
	resolver := &fakeArtistContentProvider{
		resolveIDFn: func(_ context.Context, name string) (string, bool) {
			if name == "Che" {
				return "sc-42", true
			}
			return "", false
		},
	}
	if got := resolveArtistIDByName(context.Background(), resolver, "Che"); got != "sc-42" {
		t.Errorf("resolver hit = %q, want sc-42", got)
	}
	if got := resolveArtistIDByName(context.Background(), resolver, "Unknown"); got != "" {
		t.Errorf("resolver miss = %q, want empty (sit out, don't guess)", got)
	}
	if got := resolveArtistIDByName(context.Background(), resolver, ""); got != "" {
		t.Errorf("empty name = %q, want empty", got)
	}
	if got := resolveArtistIDByName(context.Background(), plainArtistProvider{}, "Che"); got != "" {
		t.Errorf("non-resolver provider = %q, want empty", got)
	}
}

type countingIdentityResolver struct {
	mu     sync.Mutex
	calls  []string
	byName map[string]*ports.ArtistIdentity
	err    error
}

func (r *countingIdentityResolver) ResolveArtistIdentity(_ context.Context, name string) (*ports.ArtistIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	if r.err != nil {
		return nil, r.err
	}
	return r.byName[name], nil
}

func disambigArtist(name string) domain.SearchResult {
	return res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
}

func TestApplyArtistDisambiguation_PreResolvedExtrasAreFree(t *testing.T) {
	resolver := &countingIdentityResolver{}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	withExtra := disambigArtist("Che")
	withExtra.Extras = map[string]any{"disambiguation": "American rapper"}
	out := svc.applyArtistDisambiguation(context.Background(), []domain.SearchResult{withExtra})

	if out[0].Subtitle != "American rapper" {
		t.Errorf("subtitle = %q, want the pre-resolved extra applied", out[0].Subtitle)
	}
	if len(resolver.calls) != 0 {
		t.Errorf("live lookups = %v, want none (extras are free)", resolver.calls)
	}
}

func TestApplyArtistDisambiguation_LiveLookupFillsSubtitleMBIDAndExtras(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{
		"Che": {MBID: "mb-che", Disambiguation: "American rapper"},
	}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	out := svc.applyArtistDisambiguation(context.Background(), []domain.SearchResult{disambigArtist("Che")})

	if out[0].Subtitle != "American rapper" || out[0].MBID != "mb-che" {
		t.Errorf("result = subtitle %q mbid %q, want filled from MB", out[0].Subtitle, out[0].MBID)
	}
	if out[0].Extras["disambiguation"] != "American rapper" {
		t.Errorf("extras = %v, want the disambiguation memoized", out[0].Extras)
	}
}

func TestApplyArtistDisambiguation_BudgetCapsLiveLookups(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	in := []domain.SearchResult{
		disambigArtist("A"), disambigArtist("B"), disambigArtist("A"),
		disambigArtist("C"), disambigArtist("D"), disambigArtist("E"),
	}
	svc.applyArtistDisambiguation(context.Background(), in)

	if len(resolver.calls) != disambigMaxLookups {
		t.Errorf("live lookups = %d (%v), want the %d budget", len(resolver.calls), resolver.calls, disambigMaxLookups)
	}
}

func TestApplyArtistDisambiguation_SkipsNonArtistsAndFilledSubtitles(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	trk := deezerTrack("Che", "Someone", 50)
	named := disambigArtist("Che")
	named.Subtitle = "already set"
	out := svc.applyArtistDisambiguation(context.Background(), []domain.SearchResult{trk, named})

	if len(resolver.calls) != 0 {
		t.Errorf("live lookups = %v, want none (nothing eligible)", resolver.calls)
	}
	if out[1].Subtitle != "already set" {
		t.Errorf("filled subtitle overwritten: %q", out[1].Subtitle)
	}
}

func TestApplyArtistDisambiguation_ResolverErrorLeavesResultUntouched(t *testing.T) {
	resolver := &countingIdentityResolver{err: errors.New("mb down")}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	out := svc.applyArtistDisambiguation(context.Background(), []domain.SearchResult{disambigArtist("Che")})
	if out[0].Subtitle != "" || out[0].MBID != "" {
		t.Errorf("errored lookup must leave the result untouched, got %+v", out[0])
	}
}

func TestIdentityVerifier_ForgetNilSafe(t *testing.T) {
	var v *IdentityVerifier
	v.Forget("any-mbid")
}
