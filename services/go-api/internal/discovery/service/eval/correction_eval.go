package eval

import (
	"context"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

type VocabularyLookup interface {
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

func IsRecognizedTerm(ctx context.Context, v VocabularyLookup, term string) bool {
	norm := textnorm.NormalizeForMatch(term)
	if norm == "" {
		return false
	}
	candidates, err := v.FindClosest(ctx, norm, 5)
	if err != nil {
		return false
	}
	for _, c := range candidates {
		if c.TermNorm == norm {
			return true
		}
	}
	return false
}

type Corrector interface {
	Correct(ctx context.Context, query string) *service.CorrectionResult
	CorrectAggressive(ctx context.Context, query string) *service.CorrectionResult
}

type CorrectionReport struct {
	Terms        int             `json:"terms"`         // precision denominator
	TyposTested  int             `json:"typos_tested"`  // recall denominator
	Recovered    int             `json:"recovered"`     // typo corrected back to its source term
	NotRecovered int             `json:"not_recovered"` // typo not corrected (or to the wrong term)
	Corrupted    int             `json:"corrupted"`     // a valid term the corrector rewrote — false positive
	RecallMisses []FailureRecord `json:"recall_misses"`
	Corruptions  []FailureRecord `json:"corruptions"`
}

func (r CorrectionReport) RecallRate() float64 {
	if r.TyposTested == 0 {
		return 0
	}
	return float64(r.Recovered) / float64(r.TyposTested)
}

func (r CorrectionReport) PrecisionRate() float64 {
	if r.Terms == 0 {
		return 0
	}
	return float64(r.Terms-r.Corrupted) / float64(r.Terms)
}

func RunCorrectionEval(ctx context.Context, terms []string, c Corrector, typosPerTerm int) CorrectionReport {
	if typosPerTerm < 1 {
		typosPerTerm = 1
	}
	report := CorrectionReport{RecallMisses: []FailureRecord{}, Corruptions: []FailureRecord{}}

	for _, raw := range terms {
		term := textnorm.NormalizeForMatch(raw)
		if term == "" {
			continue
		}
		report.Terms++

		if res := c.Correct(ctx, term); res != nil && textnorm.NormalizeForMatch(res.Corrected) != term {
			report.Corrupted++
			attrs := QueryAttrs(term)
			attrs["corrected_to"] = res.Corrected
			report.Corruptions = append(report.Corruptions, FailureRecord{Query: term, Reason: "corrupted_valid_query", Attrs: attrs})
		}

		for _, typo := range syntheticTypos(term, typosPerTerm) {
			report.TyposTested++
			res := c.CorrectAggressive(ctx, typo)
			if res != nil && textnorm.NormalizeForMatch(res.Corrected) == term {
				report.Recovered++
				continue
			}
			report.NotRecovered++
			attrs := QueryAttrs(term)
			attrs["typo"] = typo
			if res != nil {
				attrs["corrected_to"] = res.Corrected
			}
			report.RecallMisses = append(report.RecallMisses, FailureRecord{Query: typo, Reason: "not_recovered", Attrs: attrs})
		}
	}
	return report
}

func syntheticTypos(term string, k int) []string {
	letters := []int{}
	runes := []rune(term)
	for i, r := range runes {
		if r != ' ' {
			letters = append(letters, i)
		}
	}
	if len(letters) < 2 {
		return nil
	}

	seed := 0
	for _, r := range runes {
		seed = seed*31 + int(r)
	}
	if seed < 0 {
		seed = -seed
	}

	out := []string{}
	seen := map[string]bool{term: true}
	for i := 0; i < k*3 && len(out) < k; i++ {
		pos := letters[(seed+i)%len(letters)]
		var typo string
		switch i % 3 {
		case 0:
			typo = string(runes[:pos]) + string(neighborRune(runes[pos])) + string(runes[pos+1:])
		case 1:
			typo = string(runes[:pos]) + string(runes[pos+1:])
		default:
			typo = string(runes[:pos+1]) + string(runes[pos]) + string(runes[pos+1:])
		}
		typo = strings.TrimSpace(typo)
		if typo == "" || seen[typo] {
			continue
		}
		seen[typo] = true
		out = append(out, typo)
	}
	return out
}

func neighborRune(r rune) rune {
	neighbors := map[rune]rune{
		'a': 's', 's': 'd', 'd': 'f', 'f': 'g', 'g': 'h', 'h': 'j', 'j': 'k', 'k': 'l', 'l': 'k',
		'q': 'w', 'w': 'e', 'e': 'r', 'r': 't', 't': 'y', 'y': 'u', 'u': 'i', 'i': 'o', 'o': 'p', 'p': 'o',
		'z': 'x', 'x': 'c', 'c': 'v', 'v': 'b', 'b': 'n', 'n': 'm', 'm': 'n',
	}
	if n, ok := neighbors[r]; ok {
		return n
	}
	if r >= 'a' && r < 'z' {
		return r + 1
	}
	return 'a'
}
