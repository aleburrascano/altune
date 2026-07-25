package eval

import (
	"context"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/discovery/service"
)

type queryCorrector interface {
	Correct(ctx context.Context, query string) *service.CorrectionResult
}

type GapStrength int

const (
	GapStrengthUnknown GapStrength = iota
	GapStrong
	GapWeak
	GapAbandoned
)

func (g GapStrength) MarshalJSON() ([]byte, error) {
	return []byte(`"` + g.String() + `"`), nil
}

func (g GapStrength) String() string {
	switch g {
	case GapStrong:
		return "strong"
	case GapWeak:
		return "weak"
	case GapAbandoned:
		return "abandoned"
	default:
		return "unknown"
	}
}

type CoverageGap struct {
	QueryNorm string      `json:"query_norm"`
	Count     int         `json:"count"`
	Strength  GapStrength `json:"strength"`
}

type CoverageReportA struct {
	Strong          []CoverageGap `json:"strong"`
	Weak            []CoverageGap `json:"weak"`
	Abandoned       []CoverageGap `json:"abandoned"`
	FilteredAsTypos int           `json:"filtered_as_typos"`
}

type CoverageSignalAService struct {
	events    ports.EventQuery
	corrector queryCorrector
}

func NewCoverageSignalAService(events ports.EventQuery, corrector queryCorrector) *CoverageSignalAService {
	return &CoverageSignalAService{events: events, corrector: corrector}
}

func (s *CoverageSignalAService) Execute(ctx context.Context, since time.Time, limit int) (*CoverageReportA, error) {
	zero, err := s.events.ZeroResultQueries(ctx, since, limit)
	if err != nil {
		return nil, fmt.Errorf("coverage signal a: zero-result queries: %w", err)
	}

	report := &CoverageReportA{Strong: []CoverageGap{}, Weak: []CoverageGap{}, Abandoned: []CoverageGap{}}
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

	abandoned, err := s.events.AbandonedSearches(ctx, since, limit)
	if err != nil {
		return nil, fmt.Errorf("coverage signal a: abandoned searches: %w", err)
	}
	for _, qc := range abandoned {
		report.Abandoned = append(report.Abandoned, CoverageGap{
			QueryNorm: qc.QueryNorm,
			Count:     qc.Count,
			Strength:  GapAbandoned,
		})
	}

	return report, nil
}

func (s *CoverageSignalAService) isCorrectableTypo(ctx context.Context, queryNorm string) bool {
	if s.corrector == nil {
		return false
	}
	return s.corrector.Correct(ctx, queryNorm) != nil
}
