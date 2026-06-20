package service

import (
	"context"
	"slices"
	"strings"
	"sync/atomic"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

// LibraryEntity is one unique (title, artist) pair drawn from the catalog. The
// eval treats the user's own library as ground truth: a search for
// "artist title" should surface that exact track in the top results.
type LibraryEntity struct {
	Title  string `json:"title"`
	Artist string `json:"artist"`
}

// Searcher is the slice of the search pipeline the eval consumes. The CLI wraps
// the real SearchMusicService; tests pass a canned fake. Defined here (consumer
// side) so the eval never depends on the full Execute signature.
type Searcher interface {
	Search(ctx context.Context, query string) ([]domain.SearchResult, error)
}

// EvalOutcome is the verdict for one entity. Zero value is unknown/invalid.
type EvalOutcome int

const (
	EvalOutcomeUnknown EvalOutcome = iota
	EvalPass                       // the entity appeared within the top-K window
	EvalFailWrongTop               // results returned but the entity was not in the top-K
	EvalFailNoResults              // search returned nothing (or errored)
	EvalSkipped                    // no artist — an "artist title" query can't be formed
)

func (o EvalOutcome) String() string {
	switch o {
	case EvalPass:
		return "pass"
	case EvalFailWrongTop:
		return "fail_wrong_top"
	case EvalFailNoResults:
		return "fail_no_results"
	case EvalSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

// MarshalJSON emits the outcome as its label so the JSON report is readable.
func (o EvalOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"` + o.String() + `"`), nil
}

// ResultSummary captures what actually ranked #1 when an entity missed the window.
type ResultSummary struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

// EvalResult is the verdict plus diagnostics for one entity.
type EvalResult struct {
	Entity        LibraryEntity  `json:"entity"`
	Query         string         `json:"query"`
	Outcome       EvalOutcome    `json:"outcome"`
	MatchPosition int            `json:"match_position"`  // 0-based position the entity matched; -1 if not in top-K
	Top           *ResultSummary `json:"top,omitempty"`   // what ranked #1 when the entity wasn't #1
	Error         string         `json:"error,omitempty"` // search error, if any
}

// EvalReport is the aggregate quality-regression report. The product bar is
// "the right answer is visible in the top results", so both top-1 (strict) and
// top-K (the relaxed bar) are reported.
type EvalReport struct {
	K                 int            `json:"k"`           // the top-K window evaluated
	Total             int            `json:"total"`       // entities evaluated (includes skipped)
	Evaluated         int            `json:"evaluated"`   // total - skipped (the rate denominator)
	Top1Passed        int            `json:"top1_passed"` // entity ranked #1
	TopKPassed        int            `json:"topk_passed"` // entity within the top-K (includes top1)
	Failed            int            `json:"failed"`      // not in the top-K (or no results)
	Skipped           int            `json:"skipped"`
	FailuresByTopKind map[string]int `json:"failures_by_top_kind"` // what kind ranked #1 on a miss (incl. "none")
	Results           []EvalResult   `json:"results"`
}

// Top1Rate is top1_passed / evaluated, in [0,1].
func (r EvalReport) Top1Rate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.Top1Passed) / float64(r.Evaluated)
}

// TopKRate is topk_passed / evaluated, in [0,1].
func (r EvalReport) TopKRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.TopKPassed) / float64(r.Evaluated)
}

// RunLibraryEval searches "artist title" for every entity and checks whether the
// entity appears within the top-k results. concurrency bounds parallel searches
// against live provider rate limits (use 1 for a fake searcher in tests). k is
// the window (k=1 is strict #1). progress, if non-nil, is called as entities
// complete (throttled to ~5% steps). A per-entity search error is recorded as a
// failure, never aborting the run.
func RunLibraryEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency, k int, progress func(done, total int)) EvalReport {
	if concurrency < 1 {
		concurrency = 1
	}
	if k < 1 {
		k = 1
	}

	total := len(entities)
	step := total / 20
	if step < 1 {
		step = 1
	}

	results := make([]EvalResult, total)
	var done int32
	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	for i, entity := range entities {
		i, entity := i, entity
		g.Go(func() error {
			results[i] = evalOne(ctx, entity, searcher, k)
			n := int(atomic.AddInt32(&done, 1))
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait() // evalOne never returns an error through the group

	return aggregate(results, k)
}

func evalOne(ctx context.Context, entity LibraryEntity, searcher Searcher, k int) EvalResult {
	if strings.TrimSpace(entity.Artist) == "" {
		return EvalResult{Entity: entity, Outcome: EvalSkipped, MatchPosition: -1}
	}

	query := entity.Artist + " " + entity.Title
	res := EvalResult{Entity: entity, Query: query, MatchPosition: -1}

	shown, err := searcher.Search(ctx, query)
	if err != nil {
		res.Outcome = EvalFailNoResults
		res.Error = err.Error()
		return res
	}
	if len(shown) == 0 {
		res.Outcome = EvalFailNoResults
		return res
	}

	limit := k
	if limit > len(shown) {
		limit = len(shown)
	}
	for i := 0; i < limit; i++ {
		if matchesEntity(shown[i], entity) {
			res.Outcome = EvalPass
			res.MatchPosition = i
			return res
		}
	}

	res.Outcome = EvalFailWrongTop
	res.Top = &ResultSummary{
		Kind:     shown[0].Kind.String(),
		Title:    shown[0].Title,
		Subtitle: shown[0].Subtitle,
	}
	return res
}

// matchesEntity is true when the result is the track for this entity.
// Providers routinely embed the artist (and track numbers) in the track title —
// "A-Ha - Take On Me", "07-The Best Was Yet To Come" — and sometimes list a
// re-uploader as the subtitle. So the entity title is matched as a contiguous
// token run within the result title, and the artist may appear in either the
// subtitle or the title. Token-boundary matching avoids short titles like "Go"
// matching inside "Going".
func matchesEntity(r domain.SearchResult, entity LibraryEntity) bool {
	if r.Kind != domain.ResultKindTrack {
		return false
	}
	rt := NormalizeForMatch(r.Title)
	et := NormalizeForMatch(entity.Title)
	ea := NormalizeForMatch(entity.Artist)

	if !containsTokens(rt, et) {
		return false
	}
	return containsTokens(NormalizeForMatch(r.Subtitle), ea) || containsTokens(rt, ea)
}

// containsTokens reports whether want's tokens appear as a contiguous run within
// have's tokens (exact match included).
func containsTokens(have, want string) bool {
	if want == "" {
		return false
	}
	h := strings.Fields(have)
	w := strings.Fields(want)
	if len(w) > len(h) {
		return false
	}
	for i := 0; i+len(w) <= len(h); i++ {
		if slices.Equal(h[i:i+len(w)], w) {
			return true
		}
	}
	return false
}

func aggregate(results []EvalResult, k int) EvalReport {
	report := EvalReport{
		K:                 k,
		Total:             len(results),
		FailuresByTopKind: map[string]int{},
		Results:           results,
	}
	for _, res := range results {
		switch res.Outcome {
		case EvalPass:
			report.TopKPassed++
			if res.MatchPosition == 0 {
				report.Top1Passed++
			}
		case EvalSkipped:
			report.Skipped++
		case EvalFailWrongTop:
			report.Failed++
			report.FailuresByTopKind[res.Top.Kind]++
		case EvalFailNoResults:
			report.Failed++
			report.FailuresByTopKind["none"]++
		}
	}
	report.Evaluated = report.Total - report.Skipped
	return report
}
