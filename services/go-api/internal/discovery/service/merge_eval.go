package service

// Merge harness — library-as-truth precision/recall (plan 2026-06-24-001, Phase 1;
// recall metric redefined 2026-06-24 after the live-data test).
//
// Oracle: the user's own library. For each owned track we search "artist title"
// and inspect the merged result list (Merge has already run). Two black-box
// signals fall out:
//
//   - UNDER-MERGE (recall, gated, lower is better): the FIRST version counted
//     only PROVABLE duplicates — two result rows that share an identifier (ISRC/
//     MBID) or have identical canonical (title, artist, kind) yet appear as
//     separate entities. That is exactly what Merge's own contract says it must
//     collapse (merge.go: identifier, then exact canonical title+subtitle), so a
//     leftover duplicate is a real bug. This deliberately does NOT count the many
//     genuinely-distinct uploads providers (esp. SoundCloud) return for one query
//     — "redrum" and "redrum sped up" are different recordings and correctly stay
//     apart. (The original collapse_rate conflated those and read ~5% on real
//     data; this is the honest replacement.)
//
//   - OVER-MERGE (precision, gated, lower is better): a single result entity that
//     represents two DISTINCT owned tracks — Merge folded two recordings into one.
//     Tracked across the corpus by result signature.
//
// Entities the search never finds are a COVERAGE miss, not a merge miss, and are
// excluded from the denominators.

import (
	"context"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

// MergeResult is the per-entity verdict plus diagnostics.
type MergeResult struct {
	Entity            LibraryEntity `json:"entity"`
	Query             string        `json:"query"`
	Found             bool          `json:"found"`              // entity matched at least one result
	ResultsSeen       int           `json:"results_seen"`       // result rows returned for this query
	UnderMergeIncidents int         `json:"under_merge_incidents"` // provable dups left unmerged
	UnderMergeExample string        `json:"under_merge_example,omitempty"`
}

// MergeReport is the aggregate precision/recall report.
type MergeReport struct {
	Total               int           `json:"total"`
	Evaluated           int           `json:"evaluated"`             // queries that returned results
	NoMatch             int           `json:"no_match"`              // entity not found — coverage miss
	Skipped             int           `json:"skipped"`               // no artist
	ResultsSeen         int           `json:"results_seen"`          // under-merge denominator (all rows)
	UnderMergeIncidents int           `json:"under_merge_incidents"` // provable dups left unmerged
	CleanQueries        int           `json:"clean_queries"`         // queries with zero under-merge
	DistinctSeen        int           `json:"distinct_seen"`         // over-merge denominator
	OverMerged          int           `json:"over_merged"`
	UnderMergeExamples  []string      `json:"under_merge_examples,omitempty"`
	OverMergeExamples   []string      `json:"over_merge_examples,omitempty"`
	Results             []MergeResult `json:"results"`
}

// UnderMergeRate is provable-unmerged-duplicates / results_seen, in [0,1].
// Gated, lower is better. ~0 means Merge collapsed everything it provably could.
func (r MergeReport) UnderMergeRate() float64 {
	if r.ResultsSeen == 0 {
		return 0
	}
	return float64(r.UnderMergeIncidents) / float64(r.ResultsSeen)
}

// OverMergeRate is over_merged / distinct_seen, in [0,1]. Gated, lower is better.
func (r MergeReport) OverMergeRate() float64 {
	if r.DistinctSeen == 0 {
		return 0
	}
	return float64(r.OverMerged) / float64(r.DistinctSeen)
}

// CleanMergeRate is the share of evaluated queries with zero under-merge —
// reported for readability (not gated; under_merge_rate is the gate).
func (r MergeReport) CleanMergeRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.CleanQueries) / float64(r.Evaluated)
}

// RunMergeEval searches "artist title" for every entity, detects provable
// under-merges per query, and accumulates the cross-corpus over-merge signal.
func RunMergeEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency int, progress func(done, total int)) MergeReport {
	if concurrency < 1 {
		concurrency = 1
	}
	total := len(entities)
	step := total / 20
	if step < 1 {
		step = 1
	}

	results := make([]MergeResult, total)
	var mu sync.Mutex
	sigOwners := map[string]map[string]bool{}
	var done int

	g := new(errgroup.Group)
	g.SetLimit(concurrency)
	for i, entity := range entities {
		i, entity := i, entity
		g.Go(func() error {
			results[i] = mergeEvalOne(ctx, entity, searcher, &mu, sigOwners)
			mu.Lock()
			done++
			n := done
			mu.Unlock()
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()

	return aggregateMerge(results, sigOwners)
}

func mergeEvalOne(ctx context.Context, entity LibraryEntity, searcher Searcher, mu *sync.Mutex, sigOwners map[string]map[string]bool) MergeResult {
	res := MergeResult{Entity: entity}
	if entity.Artist == "" {
		return res // skipped (no artist) — Found stays false, Query empty
	}
	query := entity.Artist + " " + entity.Title
	res.Query = query

	shown, err := searcher.Search(ctx, query)
	if err != nil || len(shown) == 0 {
		return res // no_match
	}
	res.Found = true
	res.ResultsSeen = len(shown)

	// Under-merge: provable duplicates Merge should have collapsed.
	incidents, example := detectUnderMerge(shown)
	res.UnderMergeIncidents = incidents
	res.UnderMergeExample = example

	// Over-merge: record which owned titles claim each matching result signature.
	ownerTitle := textnorm.NormalizeForMatch(entity.Title)
	for _, r := range shown {
		if !matchesEntity(r, entity) {
			continue
		}
		sig := resultSignature(r)
		mu.Lock()
		if sigOwners[sig] == nil {
			sigOwners[sig] = map[string]bool{}
		}
		sigOwners[sig][ownerTitle] = true
		mu.Unlock()
	}
	return res
}

// detectUnderMerge groups a query's results by a PROVABLE identity key and counts
// entities beyond the first in any group — duplicates Merge failed to collapse.
// A result keys on its strongest identifier (isrc, then mbid); lacking those, on
// the exact canonical (kind, title, subtitle) that merge.go merges on. Two rows
// only collide when they produce the same key, so distinct versions (whose
// canonical titles differ) never count.
func detectUnderMerge(results []domain.SearchResult) (incidents int, example string) {
	groups := map[string][]domain.SearchResult{}
	for _, r := range results {
		groups[provableIdentityKey(r)] = append(groups[provableIdentityKey(r)], r)
	}
	for _, group := range groups {
		if len(group) > 1 {
			incidents += len(group) - 1
			if example == "" {
				example = group[0].Kind.String() + " " + group[0].Title + " — " + group[0].Subtitle
			}
		}
	}
	return incidents, example
}

// provableIdentityKey is the key two results must share for Merge to have been
// obligated to collapse them: same ISRC, or same MBID, or — lacking identifiers —
// identical canonical kind+title+subtitle.
func provableIdentityKey(r domain.SearchResult) string {
	if isrc := stringExtra(r, "isrc"); isrc != "" {
		return "isrc:" + isrc
	}
	if mbid := stringExtra(r, "mbid"); mbid != "" {
		return "mbid:" + mbid
	}
	return "t:" + r.Kind.String() + "|" + textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
}

// resultSignature is a stable identity for a result entity across queries: its
// strongest source ref, falling back to canonical title+subtitle.
func resultSignature(r domain.SearchResult) string {
	if len(r.Sources) > 0 && r.Sources[0].ExternalID != "" {
		return r.Sources[0].Provider.String() + ":" + r.Sources[0].ExternalID
	}
	return r.Kind.String() + "|" + textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
}

func aggregateMerge(results []MergeResult, sigOwners map[string]map[string]bool) MergeReport {
	report := MergeReport{Total: len(results), Results: results}
	for _, r := range results {
		switch {
		case r.Query == "":
			report.Skipped++
		case !r.Found:
			report.NoMatch++
		default:
			report.Evaluated++
			report.ResultsSeen += r.ResultsSeen
			report.UnderMergeIncidents += r.UnderMergeIncidents
			if r.UnderMergeIncidents == 0 {
				report.CleanQueries++
			} else if len(report.UnderMergeExamples) < 20 {
				report.UnderMergeExamples = append(report.UnderMergeExamples, r.Query+" → "+r.UnderMergeExample)
			}
		}
	}

	for sig, owners := range sigOwners {
		report.DistinctSeen++
		if len(owners) > 1 {
			report.OverMerged++
			if len(report.OverMergeExamples) < 20 {
				report.OverMergeExamples = append(report.OverMergeExamples, sig+" ← "+joinKeys(owners))
			}
		}
	}
	return report
}

func joinKeys(m map[string]bool) string {
	out := ""
	for k := range m {
		if out != "" {
			out += " | "
		}
		out += k
	}
	return out
}
