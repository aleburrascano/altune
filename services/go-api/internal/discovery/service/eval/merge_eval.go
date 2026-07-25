package eval

import (
	"context"
	"sync"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

type MergeResult struct {
	Entity              LibraryEntity `json:"entity"`
	Query               string        `json:"query"`
	Found               bool          `json:"found"`
	ResultsSeen         int           `json:"results_seen"`
	UnderMergeIncidents int           `json:"under_merge_incidents"`
	UnderMergeExample   string        `json:"under_merge_example,omitempty"`
}

type MergeReport struct {
	Total               int           `json:"total"`
	Evaluated           int           `json:"evaluated"`
	NoMatch             int           `json:"no_match"`
	Skipped             int           `json:"skipped"`
	ResultsSeen         int           `json:"results_seen"`
	UnderMergeIncidents int           `json:"under_merge_incidents"`
	CleanQueries        int           `json:"clean_queries"`
	DistinctSeen        int           `json:"distinct_seen"`
	OverMerged          int           `json:"over_merged"`
	UnderMergeExamples  []string      `json:"under_merge_examples,omitempty"`
	OverMergeExamples   []string      `json:"over_merge_examples,omitempty"`
	Results             []MergeResult `json:"results"`
}

func (r MergeReport) UnderMergeRate() float64 {
	if r.ResultsSeen == 0 {
		return 0
	}
	return float64(r.UnderMergeIncidents) / float64(r.ResultsSeen)
}

func (r MergeReport) OverMergeRate() float64 {
	if r.DistinctSeen == 0 {
		return 0
	}
	return float64(r.OverMerged) / float64(r.DistinctSeen)
}

func (r MergeReport) CleanMergeRate() float64 {
	if r.Evaluated == 0 {
		return 0
	}
	return float64(r.CleanQueries) / float64(r.Evaluated)
}

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
		return res
	}
	query := entity.Artist + " " + entity.Title
	res.Query = query

	shown, err := searcher.Search(ctx, query)
	if err != nil || len(shown) == 0 {
		return res
	}
	res.Found = true
	res.ResultsSeen = len(shown)

	incidents, example := detectUnderMerge(shown)
	res.UnderMergeIncidents = incidents
	res.UnderMergeExample = example

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

func provableIdentityKey(r domain.SearchResult) string {
	if r.ISRC != "" {
		return "isrc:" + r.ISRC
	}
	if r.MBID != "" {
		return "mbid:" + r.MBID
	}
	return "t:" + r.Kind.String() + "|" + textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
}

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
