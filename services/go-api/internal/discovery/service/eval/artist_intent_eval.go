package eval

// Artist-intent eval — the corpus the library eval is structurally blind to.
//
// The library eval (library_eval.go) only ever passes on a TRACK
// (matchesEntity hard-requires ResultKindTrack), so "search a bare artist name,
// expect the artist card on top" is unmeasured. This harness fills that gap: for
// each distinct library artist it queries the bare name and asks whether an
// artist card named that surfaces in the top-K.
//
// The headline split is the load-bearing part. A miss is one of two very
// different things, and the ranker can only fix one:
//
//   - BURIED  — an artist card named X exists in the result set but ranks below
//     K while a same-name TRACK ranks within it. This is the kind-blindness bug
//     (bare-token relevance ties → multi-source/RRF tiebreak favors the
//     better-covered track). The ranker CAN fix this.
//   - ABSENT  — no artist card named X surfaced anywhere. A recall / identity
//     gap (the queried artist never entered the candidate set, or merged into a
//     same-name other). The ranker CANNOT fix this — no reorder surfaces a
//     result that isn't there.
//
// Keeping them apart is what stops a "boost artists" change from looking like it
// worked when the real failure was recall.

import (
	"context"
	"sync/atomic"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	"golang.org/x/sync/errgroup"
)

// ArtistIntentOutcome is the verdict for one bare-artist-name query.
type ArtistIntentOutcome int

const (
	ArtistIntentUnknown   ArtistIntentOutcome = iota
	ArtistIntentPass                          // an artist card named X is within the top-K
	ArtistIntentBuried                        // artist card exists below K AND a same-name track is within K (the ranker bug)
	ArtistIntentBelowK                        // artist card exists below K but no same-name track usurped it (album/other domination or pure rank)
	ArtistIntentAbsent                        // no artist card named X anywhere — recall/identity gap, not a ranking bug
	ArtistIntentNoResults                     // search returned nothing or errored
	ArtistIntentSkipped                       // name normalizes to empty (symbol-only) — cannot be matched
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

// MarshalJSON emits the outcome as its label so the JSON report is readable.
func (o ArtistIntentOutcome) MarshalJSON() ([]byte, error) {
	return []byte(`"` + o.String() + `"`), nil
}

// ArtistIntentResult is the verdict plus diagnostics for one artist query.
type ArtistIntentResult struct {
	Artist        string              `json:"artist"`
	Outcome       ArtistIntentOutcome `json:"outcome"`
	ArtistPos     int                 `json:"artist_pos"`      // 0-based position the artist card landed; -1 if absent
	FirstTrackPos int                 `json:"first_track_pos"` // 0-based position of the first same-name track; -1 if none
	Top           *ResultSummary      `json:"top,omitempty"`   // what ranked #1 (when the artist wasn't)
	Error         string              `json:"error,omitempty"`
}

// ArtistIntentReport is the aggregate artist-intent quality report.
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

// Top1Rate is artist-#1 / evaluated.
func (r ArtistIntentReport) Top1Rate() float64 { return rate(r.Top1Passed, r.Evaluated) }

// TopKRate is artist-in-top-K / evaluated — the product bar (higher is better).
func (r ArtistIntentReport) TopKRate() float64 { return rate(r.TopKPassed, r.Evaluated) }

// BuriedRate is the kind-blindness signal: artist present but out-ranked by a
// same-name track / evaluated (lower is better). The number the ranker fix moves.
func (r ArtistIntentReport) BuriedRate() float64 { return rate(r.Buried, r.Evaluated) }

// AbsentRate is the recall gap: artist card never surfaced / evaluated (lower is
// better). The ranker cannot move this — only fan-out/identity work can.
func (r ArtistIntentReport) AbsentRate() float64 { return rate(r.Absent, r.Evaluated) }

func rate(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}

// RunArtistIntentEval queries each bare artist name and classifies whether an
// artist card named that surfaces in the top-K. concurrency bounds parallel
// live-provider searches; k is the window (k=1 is strict #1).
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

// evalOneArtist runs one bare-name query and classifies the outcome. It scans
// the FULL result list (not just top-K) so it can tell "buried" (artist present,
// out-ranked) from "absent" (artist never surfaced).
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

// ---- HarnessReport conformance -----------------------------------------

var _ HarnessReport = ArtistIntentReport{}

// Metrics gates the product bar (topk_rate, higher better) and the two failure
// modes kept distinct: buried_rate (the ranker bug, lower better) and
// absent_rate (the recall gap, lower better). top1_rate is recorded for history.
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

// Failures emits one attributed record per non-pass, tagged with the outcome so
// the buried/absent split survives into the failure log and its slices.
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
