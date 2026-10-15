package eval

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/release"
	"altune/go-api/internal/shared/songfacts"
	"altune/go-api/internal/shared/textnorm"
	"altune/go-api/internal/shared/versiontag"
	"context"
	"slices"
	"strings"
	"sync"
)

type MergeResult struct {
	Entity              LibraryEntity `json:"entity"`
	Query               string        `json:"query"`
	Found               bool          `json:"found"`
	Errored             bool          `json:"errored,omitempty"`
	ResultsSeen         int           `json:"results_seen"`
	UnderMergeIncidents int           `json:"under_merge_incidents"`
	UnderMergeExample   string        `json:"under_merge_example,omitempty"`
}

type MergeReport struct {
	Total               int           `json:"total"`
	Evaluated           int           `json:"evaluated"`
	NoMatch             int           `json:"no_match"`
	Errored             int           `json:"errored,omitempty"`
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

func (r MergeReport) UnderMergeRate() float64 { return rate(r.UnderMergeIncidents, r.ResultsSeen) }

func (r MergeReport) OverMergeRate() float64 { return rate(r.OverMerged, r.DistinctSeen) }

func (r MergeReport) CleanMergeRate() float64 { return rate(r.CleanQueries, r.Evaluated) }

func RunMergeEval(ctx context.Context, entities []LibraryEntity, searcher Searcher, concurrency int, progress func(done, total int)) MergeReport {
	results := make([]MergeResult, len(entities))
	var mu sync.Mutex
	sigOwners := map[string]map[string]bool{}

	runParallel(entities, concurrency, progress, func(i int, entity LibraryEntity) {
		results[i] = mergeEvalOne(ctx, entity, entities, searcher, &mu, sigOwners)
	})

	return aggregateMerge(results, sigOwners)
}

func mergeEvalOne(ctx context.Context, entity LibraryEntity, corpus []LibraryEntity, searcher Searcher, mu *sync.Mutex, sigOwners map[string]map[string]bool) MergeResult {
	res := MergeResult{Entity: entity}
	if entity.Artist == "" {
		return res
	}
	query := entity.Artist + " " + entity.Title
	res.Query = query

	shown, err := searcher.Search(ctx, query)
	if err != nil {
		res.Errored = true
		return res
	}
	if len(shown) == 0 {
		return res
	}
	res.Found = true
	res.ResultsSeen = len(shown)

	incidents, example := detectUnderMerge(shown)
	res.UnderMergeIncidents = incidents
	res.UnderMergeExample = example

	for _, r := range shown {
		if !matchesEntity(r, entity) {
			continue
		}
		sig := resultSignature(r)
		mu.Lock()
		if sigOwners[sig] == nil {
			sigOwners[sig] = map[string]bool{}
		}
		sigOwners[sig][ownerKey(entity)] = true
		for _, other := range claimedOthers(r, entity, corpus) {
			sigOwners[sig][ownerKey(other)] = true
		}
		mu.Unlock()
	}
	return res
}

func ownerKey(e LibraryEntity) string {
	return textnorm.NormalizeForMatch(e.Title) + "|" + strings.Join(versiontag.RecordingLabels(e.Title), "+") + "|" + textnorm.NormalizeForMatch(e.Artist)
}

func claimedOthers(r domain.SearchResult, entity LibraryEntity, corpus []LibraryEntity) []LibraryEntity {
	if entity.Known == nil {
		return nil
	}
	var out []LibraryEntity
	for _, other := range corpus {
		if other.Known == nil || sharesISRC(other.Known, entity.Known) {
			continue
		}
		if matchesEntity(r, LibraryEntity{Title: other.Title, Artist: other.Artist}) {
			out = append(out, other)
		}
	}
	return out
}

func sharesISRC(a, b *KnownRecording) bool {
	for _, isrc := range a.ISRCs {
		if slices.Contains(b.ISRCs, isrc) {
			return true
		}
	}
	return false
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
	labels := versionLabelKey(r)
	if isrc := songfacts.CanonicalISRC(r.ISRC); isrc != "" {
		return "isrc:" + isrc + "|" + labels
	}
	if r.IdentityMBID() != "" {
		return "mbid:" + strings.ToLower(r.IdentityMBID()) + "|" + labels
	}
	return "t:" + r.Kind.String() + "|" + textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle) + "|" + labels
}

func versionLabelKey(r domain.SearchResult) string {
	return strings.Join(ResultLabels(r.Kind, r.Title), "+")
}

func resultSignature(r domain.SearchResult) string {
	if len(r.Sources) > 0 && r.Sources[0].ExternalID != "" {
		return release.SourceKey(r.Sources[0].Provider, r.Sources[0].ExternalID)
	}
	return r.Kind.String() + "|" + textnorm.NormalizeForMatch(r.Title) + "|" + textnorm.NormalizeForMatch(r.Subtitle)
}

func aggregateMerge(results []MergeResult, sigOwners map[string]map[string]bool) MergeReport {
	report := MergeReport{Total: len(results), Results: results}
	for _, r := range results {
		switch {
		case r.Query == "":
			report.Skipped++
		case r.Errored:
			report.Errored++
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
