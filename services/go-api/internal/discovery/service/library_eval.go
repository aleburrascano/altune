package service

import (
	"context"
	"strings"

	"altune/go-api/internal/discovery/domain"

	"golang.org/x/sync/errgroup"
)

// LibraryEntity is one unique (title, artist) pair drawn from the catalog. The
// eval treats the user's own library as ground truth: a search for
// "artist title" should rank that exact track #1.
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
	EvalPass                       // the entity ranked #1
	EvalFailWrongTop               // results returned but #1 was a different entity
	EvalFailNoResults              // search returned nothing (or errored)
	EvalSkipped                    // no artist — an "artist title" query can't be formed
)

// MarshalJSON emits the outcome as its label so the JSON report is readable.
func (o EvalOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"` + o.String() + `"`), nil
}

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

// ResultSummary captures what actually ranked #1 when an entity failed.
type ResultSummary struct {
	Kind     string `json:"kind"`
	Title    string `json:"title"`
	Subtitle string `json:"subtitle"`
}

// EvalResult is the verdict plus diagnostics for one entity.
type EvalResult struct {
	Entity  LibraryEntity  `json:"entity"`
	Query   string         `json:"query"`
	Outcome EvalOutcome    `json:"outcome"`
	Top     *ResultSummary `json:"top,omitempty"`   // what ranked #1 on a wrong-top failure
	Error   string         `json:"error,omitempty"` // search error, if any
}

// EvalReport is the aggregate quality-regression report.
type EvalReport struct {
	Total             int            `json:"total"`     // entities evaluated (includes skipped)
	Evaluated         int            `json:"evaluated"` // total - skipped (the pass-rate denominator)
	Passed            int            `json:"passed"`
	Failed            int            `json:"failed"`
	Skipped           int            `json:"skipped"`
	FailuresByTopKind map[string]int `json:"failures_by_top_kind"` // what kind beat the entity (incl. "none")
	Results           []EvalResult   `json:"results"`
}

// PassRate is passed / evaluated, in [0,1]. Zero when nothing was evaluated.
func (r EvalReport) PassRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Evaluated)
}

// RunLibraryEval searches "artist title" for every entity and asserts the entity
// ranks #1. concurrency bounds parallel searches against live provider rate
// limits (use 1 for a fake searcher in tests). A per-entity search error is
// recorded as a failure, never aborting the run.
func RunLibraryEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency int) EvalReport {
	if concurrency < 1 {
		concurrency = 1
	}

	results := make([]EvalResult, len(entities))
	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	for i, entity := range entities {
		i, entity := i, entity
		g.Go(func() error {
			results[i] = evalOne(ctx, entity, searcher)
			return nil
		})
	}
	_ = g.Wait() // evalOne never returns an error through the group

	return aggregate(results)
}

func evalOne(ctx context.Context, entity LibraryEntity, searcher Searcher) EvalResult {
	if strings.TrimSpace(entity.Artist) == "" {
		return EvalResult{Entity: entity, Outcome: EvalSkipped}
	}

	query := entity.Artist + " " + entity.Title
	res := EvalResult{Entity: entity, Query: query}

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

	if matchesEntity(shown[0], entity) {
		res.Outcome = EvalPass
		return res
	}

	res.Outcome = EvalFailWrongTop
	res.Top = &ResultSummary{
		Kind:     shown[0].Kind.String(),
		Title:    shown[0].Title,
		Subtitle: shown[0].Subtitle,
	}
	return res
}

// matchesEntity is true when the result is the track for this entity: same kind,
// matching normalized title, and the artist appears in the result's subtitle.
func matchesEntity(r domain.SearchResult, entity LibraryEntity) bool {
	if r.Kind != domain.ResultKindTrack {
		return false
	}
	if NormalizeForMatch(r.Title) != NormalizeForMatch(entity.Title) {
		return false
	}
	return strings.Contains(NormalizeForMatch(r.Subtitle), NormalizeForMatch(entity.Artist))
}

func aggregate(results []EvalResult) EvalReport {
	report := EvalReport{
		Total:             len(results),
		FailuresByTopKind: map[string]int{},
		Results:           results,
	}
	for _, res := range results {
		switch res.Outcome {
		case EvalPass:
			report.Passed++
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
