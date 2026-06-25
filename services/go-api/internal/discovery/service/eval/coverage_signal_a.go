package eval

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
)

// queryCorrector is the slice of CorrectionService the signal consumes: given a
// query, does a vocabulary correction exist? Defined here (consumer side) so the
// signal can be tested with a fake.
type queryCorrector interface {
	Correct(ctx context.Context, query string) *service.CorrectionResult
}

// GapStrength labels how confident we are that a query is a real coverage gap.
// Zero value is unknown/invalid.
type GapStrength int

const (
	GapStrengthUnknown GapStrength = iota
	GapStrong                      // zero-result and not a correctable typo
	GapWeak                        // returned results but drew no click
)

// MarshalJSON emits the strength as its label so the JSON report is readable.
func (g GapStrength) MarshalJSON() ([]byte, error) {
	return []byte(`"` + g.String() + `"`), nil
}

func (g GapStrength) String() string {
	switch g {
	case GapStrong:
		return "strong"
	case GapWeak:
		return "weak"
	default:
		return "unknown"
	}
}

// CoverageGap is a demand-weighted candidate gap query.
type CoverageGap struct {
	QueryNorm string      `json:"query_norm"`
	Count     int         `json:"count"`
	Strength  GapStrength `json:"strength"`
}

// CoverageReportA is the zero-result / abandoned-search coverage report.
type CoverageReportA struct {
	Strong          []CoverageGap `json:"strong"`            // zero-result, not typos
	Weak            []CoverageGap `json:"weak"`              // results shown, no click
	FilteredAsTypos int           `json:"filtered_as_typos"` // zero-result queries dropped by the correction filter
}

// CoverageSignalAService mines telemetry into a coverage-gap report. A
// zero-result query is a strong gap unless the corrector can fix it (then it's a
// typo, not missing coverage). A results-but-no-click query is a weak hint only.
type CoverageSignalAService struct {
	events    ports.EventQuery
	corrector queryCorrector // may be nil — then no typo filtering is applied
}

func NewCoverageSignalAService(events ports.EventQuery, corrector queryCorrector) *CoverageSignalAService {
	return &CoverageSignalAService{events: events, corrector: corrector}
}

func (s *CoverageSignalAService) Execute(ctx context.Context, since time.Time, limit int) (*CoverageReportA, error) {
	zero, err := s.events.ZeroResultQueries(ctx, since, limit)
	if err != nil {
		return nil, fmt.Errorf("coverage signal a: zero-result queries: %w", err)
	}

	report := &CoverageReportA{Strong: []CoverageGap{}, Weak: []CoverageGap{}}
	for _, qc := range zero {
		if s.isCorrectableTypo(ctx, qc.QueryNorm) {
			report.FilteredAsTypos++
			continue
		}
		report.Strong = append(report.Strong, CoverageGap{
			QueryNorm: qc.QueryNorm,
			Count:     qc.Count,
			Strength:  GapStrong,
		})
	}

	noClick, err := s.events.NonZeroNoClickQueries(ctx, since, limit)
	if err != nil {
		return nil, fmt.Errorf("coverage signal a: no-click queries: %w", err)
	}
	for _, qc := range noClick {
		report.Weak = append(report.Weak, CoverageGap{
			QueryNorm: qc.QueryNorm,
			Count:     qc.Count,
			Strength:  GapWeak,
		})
	}

	return report, nil
}

// isCorrectableTypo is true when a vocabulary correction exists for the query —
// evidence the zero result was a misspelling, not missing coverage. Offline
// approximation of "the corrected query would also be empty": if a correction
// exists, the corrected form would have returned results, so it is not a gap.
func (s *CoverageSignalAService) isCorrectableTypo(ctx context.Context, queryNorm string) bool {
	if s.corrector == nil {
		return false
	}
	return s.corrector.Correct(ctx, queryNorm) != nil
}
