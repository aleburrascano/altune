package eval

// Correction harness — synthetic perturbation (plan 2026-06-24-001, Phase 2).
//
// Oracle: a known-good vocabulary (real library/vocab terms). The harness is
// OFFLINE and DETERMINISTIC — no providers, no network fan-out, no randomness —
// so it can run anywhere and reproduce exactly.
//
//   - RECALL: perturb each known-good term with a single-edit typo, ask the
//     corrector to fix it, and check it comes back. Typos are deliberately
//     distance-1 (substitution / deletion / insertion) because the
//     CorrectionService's own maxCorrectionDist tolerates only 1 edit on short
//     terms — a distance-2 transposition would be a false negative, not a real
//     miss. recall_rate is gated, higher is better.
//
//   - PRECISION: feed each known-good term UNPERTURBED and require the corrector
//     to leave it alone. A corrector that rewrites a valid query is the costly
//     failure (the disabled pre-correction did exactly this). precision_rate is
//     gated, higher is better.
//
// Blind spot (documented): synthetic single-edit typos are not the real user
// typo distribution, and perturbing the normalized form skips pre-normalization
// noise. This harness gates the correction ALGORITHM against its own vocabulary;
// the vocabulary's own quality is a separate concern.

import (
	"context"
	"strings"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

// VocabularyLookup is the slice of the vocabulary store the correction harness's
// term-recognition step needs. Defined here (consumer side).
type VocabularyLookup interface {
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

// IsRecognizedTerm reports whether the store holds the term exactly — the
// precondition for using it as known-good vocabulary. A recall miss on a
// recognized term is a real correction failure; on an unrecognized one it would
// be a vocabulary-coverage artifact, so those are filtered out upstream.
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

// Corrector is the slice of CorrectionService the harness consumes — both the
// conservative whole-query path (precision) and the aggressive recovery path
// (recall). Defined here (consumer side) so the harness tests with a fake.
type Corrector interface {
	Correct(ctx context.Context, query string) *service.CorrectionResult
	CorrectAggressive(ctx context.Context, query string) *service.CorrectionResult
}

// CorrectionReport is the aggregate precision/recall report.
type CorrectionReport struct {
	Terms        int             `json:"terms"`         // precision denominator
	TyposTested  int             `json:"typos_tested"`  // recall denominator
	Recovered    int             `json:"recovered"`     // typo corrected back to its source term
	NotRecovered int             `json:"not_recovered"` // typo not corrected (or to the wrong term)
	Corrupted    int             `json:"corrupted"`     // a valid term the corrector rewrote — false positive
	RecallMisses []FailureRecord `json:"recall_misses"`
	Corruptions  []FailureRecord `json:"corruptions"`
}

// RecallRate is recovered / typos_tested, in [0,1].
func (r CorrectionReport) RecallRate() float64 {
	if r.TyposTested == 0 {
		return 0
	}
	return float64(r.Recovered) / float64(r.TyposTested)
}

// PrecisionRate is (terms - corrupted) / terms, in [0,1] — the share of valid
// queries the corrector correctly left untouched.
func (r CorrectionReport) PrecisionRate() float64 {
	if r.Terms == 0 {
		return 0
	}
	return float64(r.Terms-r.Corrupted) / float64(r.Terms)
}

// RunCorrectionEval perturbs each known-good term with up to typosPerTerm
// single-edit typos and measures recall + precision against the corrector.
// Deterministic: same terms in → same report out.
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

		// Precision: the corrector must not rewrite a valid term.
		if res := c.Correct(ctx, term); res != nil && textnorm.NormalizeForMatch(res.Corrected) != term {
			report.Corrupted++
			attrs := QueryAttrs(term)
			attrs["corrected_to"] = res.Corrected
			report.Corruptions = append(report.Corruptions, FailureRecord{Query: term, Reason: "corrupted_valid_query", Attrs: attrs})
		}

		// Recall: each typo must be corrected back to the term.
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

// syntheticTypos generates up to k deterministic single-edit typos of a
// normalized term (distance 1: substitution, deletion, insertion). Position and
// edit kind are derived from the term and the index, so the set is reproducible.
// Degenerate results (empty, unchanged, or term shorter than 2 letters) are
// dropped.
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
			typo = string(runes[:pos]) + string(neighborRune(runes[pos])) + string(runes[pos+1:]) // substitution
		case 1:
			typo = string(runes[:pos]) + string(runes[pos+1:]) // deletion
		default:
			typo = string(runes[:pos+1]) + string(runes[pos]) + string(runes[pos+1:]) // insertion (duplicate)
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

// neighborRune returns a deterministic QWERTY-adjacent letter for a substitution
// typo, falling back to a fixed shift for anything off the map. Lowercase only —
// the input is already normalized.
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
