package eval

import (
	"context"
	"sync/atomic"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

type ArtistIntentOutcome int

const (
	ArtistIntentUnknown ArtistIntentOutcome = iota
	ArtistIntentPass
	ArtistIntentBuried
	ArtistIntentBelowK
	ArtistIntentAbsent
	ArtistIntentNoResults
	ArtistIntentSkipped
)

func (o ArtistIntentOutcome) String() string {
	switch o {
	case ArtistIntentPass:
		return "pass"
	case ArtistIntentBuried:
		return "buried"
	case ArtistIntentBelowK:
		return "below_k"
	case ArtistIntentAbsent:
		return "absent"
	case ArtistIntentNoResults:
		return "no_results"
	case ArtistIntentSkipped:
		return "skipped"
	default:
		return "unknown"
	}
}

func (o ArtistIntentOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"` + o.String() + `"`), nil
}

type ArtistIntentResult struct {
	Artist        string              `json:"artist"`
	Outcome       ArtistIntentOutcome `json:"outcome"`
	ArtistPos     int                 `json:"artist_pos"`      // 0-based position the artist card landed; -1 if absent
	FirstTrackPos int                 `json:"first_track_pos"` // 0-based position of the first same-name track; -1 if none
	Top           *ResultSummary      `json:"top,omitempty"`   // what ranked #1 (when the artist wasn't)
	Error         string              `json:"error,omitempty"`
}

type ArtistIntentReport struct {
	Corpus     string               `json:"corpus,omitempty"` // "" = all artists, "hard" = single-token names
	K          int                  `json:"k"`
	Total      int                  `json:"total"`
	Evaluated  int                  `json:"evaluated"` // total - skipped (the rate denominator)
	Top1Passed int                  `json:"top1_passed"`
	TopKPassed int                  `json:"topk_passed"`
	Buried     int                  `json:"buried"`  // artist present, same-name track ranked above it (the bug)
	BelowK     int                  `json:"below_k"` // artist present below K, not specifically track-buried
	Absent     int                  `json:"absent"`  // artist card never surfaced (recall gap)
	NoResults  int                  `json:"no_results"`
	Skipped    int                  `json:"skipped"`
	Results    []ArtistIntentResult `json:"results"`
}

func (r ArtistIntentReport) Top1Rate() float64 { return rate(r.Top1Passed, r.Evaluated) }

func (r ArtistIntentReport) TopKRate() float64 { return rate(r.TopKPassed, r.Evaluated) }

func (r ArtistIntentReport) BuriedRate() float64 { return rate(r.Buried, r.Evaluated) }

func (r ArtistIntentReport) AbsentRate() float64 { return rate(r.Absent, r.Evaluated) }

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

func RunArtistIntentEval(ctx context.Context, artists []string, searcher Searcher, concurrency, k int, corpus string, progress func(done, total int)) ArtistIntentReport {
	if concurrency < 1 {
		concurrency = 1
	}
	if k < 1 {
		k = 1
	}

	total := len(artists)
	step := total / 20
	if step < 1 {
		step = 1
	}

	results := make([]ArtistIntentResult, total)
	var done int32
	g := new(errgroup.Group)
	g.SetLimit(concurrency)

	for i, artist := range artists {
		i, artist := i, artist
		g.Go(func() error {
			results[i] = evalOneArtist(ctx, artist, searcher, k)
			n := int(atomic.AddInt32(&done, 1))
			if progress != nil && (n%step == 0 || n == total) {
				progress(n, total)
			}
			return nil
		})
	}
	_ = g.Wait()

	report := aggregateArtistIntent(results, k)
	report.Corpus = corpus
	return report
}

func evalOneArtist(ctx context.Context, artist string, searcher Searcher, k int) ArtistIntentResult {
	name := textnorm.NormalizeForMatch(artist)
	if name == "" {
		return ArtistIntentResult{Artist: artist, Outcome: ArtistIntentSkipped, ArtistPos: -1, FirstTrackPos: -1}
	}

	res := ArtistIntentResult{Artist: artist, ArtistPos: -1, FirstTrackPos: -1}

	shown, err := searcher.Search(ctx, artist)
	if err != nil {
		res.Outcome = ArtistIntentNoResults
		res.Error = err.Error()
		return res
	}
	if len(shown) == 0 {
		res.Outcome = ArtistIntentNoResults
		return res
	}

	for i, r := range shown {
		if res.ArtistPos == -1 && r.Kind == domain.ResultKindArtist && textnorm.NormalizeForMatch(r.Title) == name {
			res.ArtistPos = i
		}
		if res.FirstTrackPos == -1 && r.Kind == domain.ResultKindTrack && textnorm.NormalizeForMatch(r.Title) == name {
			res.FirstTrackPos = i
		}
	}

	res.Top = &ResultSummary{Kind: shown[0].Kind.String(), Title: shown[0].Title, Subtitle: shown[0].Subtitle}

	switch {
	case res.ArtistPos == -1:
		res.Outcome = ArtistIntentAbsent
	case res.ArtistPos < k:
		res.Outcome = ArtistIntentPass
	case res.FirstTrackPos != -1 && res.FirstTrackPos < k:
		res.Outcome = ArtistIntentBuried
	default:
		res.Outcome = ArtistIntentBelowK
	}
	return res
}

func aggregateArtistIntent(results []ArtistIntentResult, k int) ArtistIntentReport {
	report := ArtistIntentReport{K: k, Total: len(results), Results: results}
	for _, res := range results {
		switch res.Outcome {
		case ArtistIntentPass:
			report.TopKPassed++
			if res.ArtistPos == 0 {
				report.Top1Passed++
			}
		case ArtistIntentBuried:
			report.Buried++
		case ArtistIntentBelowK:
			report.BelowK++
		case ArtistIntentAbsent:
			report.Absent++
		case ArtistIntentNoResults:
			report.NoResults++
		case ArtistIntentSkipped:
			report.Skipped++
		}
	}
	report.Evaluated = report.Total - report.Skipped
	return report
}

var _ HarnessReport = ArtistIntentReport{}

func (r ArtistIntentReport) Metrics() []NamedMetric {
	p := "artist_intent."
	if r.Corpus != "" {
		p = "artist_intent." + r.Corpus + "_"
	}
	return []NamedMetric{
		{Name: p + "top1_rate", Value: r.Top1Rate(), HigherIsBetter: true},
		{Name: p + "topk_rate", Value: r.TopKRate(), HigherIsBetter: true},
		{Name: p + "buried_rate", Value: r.BuriedRate(), HigherIsBetter: false},
		{Name: p + "absent_rate", Value: r.AbsentRate(), HigherIsBetter: false},
	}
}

func (r ArtistIntentReport) Failures() []FailureRecord {
	out := []FailureRecord{}
	for _, res := range r.Results {
		if res.Outcome == ArtistIntentPass || res.Outcome == ArtistIntentSkipped {
			continue
		}
		attrs := QueryAttrs(res.Artist)
		attrs["outcome"] = res.Outcome.String()
		if res.Top != nil {
			attrs["top_kind"] = res.Top.Kind
		}
		out = append(out, FailureRecord{Query: res.Artist, Reason: res.Outcome.String(), Attrs: attrs})
	}
	return out
}
