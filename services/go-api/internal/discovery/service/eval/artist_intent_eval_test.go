package eval

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func artistCard(name string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindArtist, Title: name}
}

func TestEvalOneArtist_Outcomes(t *testing.T) {
	tests := []struct {
		name        string
		artist      string
		results     []domain.SearchResult
		searchErr   error
		k           int
		wantOutcome ArtistIntentOutcome
		wantArtist  int
		wantTrack   int
	}{
		{
			name:        "artist card at #1 passes",
			artist:      "Drake",
			results:     []domain.SearchResult{artistCard("Drake"), evalTrack("Drake", "Someone")},
			k:           3,
			wantOutcome: ArtistIntentPass,
			wantArtist:  0,
			wantTrack:   1,
		},
		{
			name:   "artist below K with same-name track inside K is buried",
			artist: "Drake",
			results: []domain.SearchResult{
				evalTrack("Drake", "Someone"),
				album("Drake", "Other"),
				artistCard("Drake"),
			},
			k:           2,
			wantOutcome: ArtistIntentBuried,
			wantArtist:  2,
			wantTrack:   0,
		},
		{
			name:   "artist below K without a usurping track is below_k",
			artist: "Drake",
			results: []domain.SearchResult{
				album("Drake", "Other"),
				album("Drake Again", "Other"),
				artistCard("Drake"),
			},
			k:           2,
			wantOutcome: ArtistIntentBelowK,
			wantArtist:  2,
			wantTrack:   -1,
		},
		{
			name:        "no artist card anywhere is absent",
			artist:      "Drake",
			results:     []domain.SearchResult{evalTrack("Drake", "Someone")},
			k:           3,
			wantOutcome: ArtistIntentAbsent,
			wantArtist:  -1,
			wantTrack:   0,
		},
		{
			name:        "empty results are no_results",
			artist:      "Drake",
			results:     []domain.SearchResult{},
			k:           3,
			wantOutcome: ArtistIntentNoResults,
			wantArtist:  -1,
			wantTrack:   -1,
		},
		{
			name:        "search error is no_results with error recorded",
			artist:      "Drake",
			searchErr:   errors.New("providers down"),
			k:           3,
			wantOutcome: ArtistIntentNoResults,
			wantArtist:  -1,
			wantTrack:   -1,
		},
		{
			name:        "symbol-only name is skipped",
			artist:      "†††",
			k:           3,
			wantOutcome: ArtistIntentSkipped,
			wantArtist:  -1,
			wantTrack:   -1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			searcher := &fakeSearcher{
				byQuery: map[string][]domain.SearchResult{tt.artist: tt.results},
				err:     tt.searchErr,
			}
			got := evalOneArtist(context.Background(), tt.artist, searcher, tt.k)

			if got.Outcome != tt.wantOutcome {
				t.Fatalf("outcome = %v, want %v", got.Outcome, tt.wantOutcome)
			}
			if got.ArtistPos != tt.wantArtist {
				t.Errorf("ArtistPos = %d, want %d", got.ArtistPos, tt.wantArtist)
			}
			if got.FirstTrackPos != tt.wantTrack {
				t.Errorf("FirstTrackPos = %d, want %d", got.FirstTrackPos, tt.wantTrack)
			}
			if tt.searchErr != nil && got.Error == "" {
				t.Error("expected the search error to be recorded")
			}
		})
	}
}

func TestRunArtistIntentEval_AggregationAndRates(t *testing.T) {
	artists := []string{"Pass", "Buried", "Absent", "Ghost", "†††"}
	searcher := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Pass":   {artistCard("Pass")},
		"Buried": {evalTrack("Buried", "Cover Band"), album("Buried", "x"), artistCard("Buried")},
		"Absent": {evalTrack("Absent", "Someone")},
		"Ghost":  {},
	}}

	report := RunArtistIntentEval(context.Background(), artists, searcher, 1, 2, "hard", nil)

	if report.Total != 5 || report.Evaluated != 4 {
		t.Errorf("Total/Evaluated = %d/%d, want 5/4", report.Total, report.Evaluated)
	}
	if report.TopKPassed != 1 || report.Top1Passed != 1 {
		t.Errorf("TopKPassed/Top1Passed = %d/%d, want 1/1", report.TopKPassed, report.Top1Passed)
	}
	if report.Buried != 1 || report.Absent != 1 || report.NoResults != 1 || report.Skipped != 1 {
		t.Errorf("Buried/Absent/NoResults/Skipped = %d/%d/%d/%d, want 1/1/1/1",
			report.Buried, report.Absent, report.NoResults, report.Skipped)
	}
	if got := report.Top1Rate(); got != 0.25 {
		t.Errorf("Top1Rate = %v, want 0.25", got)
	}
	if got := report.BuriedRate(); got != 0.25 {
		t.Errorf("BuriedRate = %v, want 0.25", got)
	}
	if got := report.AbsentRate(); got != 0.25 {
		t.Errorf("AbsentRate = %v, want 0.25", got)
	}
	if report.Corpus != "hard" {
		t.Errorf("Corpus = %q, want hard", report.Corpus)
	}

	// Metrics carry the corpus tag and per-metric direction.
	metrics := report.Metrics()
	byName := map[string]NamedMetric{}
	for _, m := range metrics {
		byName[m.Name] = m
	}
	if m, ok := byName["artist_intent.hard_topk_rate"]; !ok || !m.HigherIsBetter {
		t.Errorf("hard_topk_rate metric = %+v, want present + higher-is-better", m)
	}
	if m, ok := byName["artist_intent.hard_buried_rate"]; !ok || m.HigherIsBetter {
		t.Errorf("hard_buried_rate metric = %+v, want present + lower-is-better", m)
	}

	// Failures: every non-pass, non-skipped outcome, tagged with its outcome.
	fails := report.Failures()
	if len(fails) != 3 {
		t.Fatalf("failures = %d, want 3 (buried, absent, no_results)", len(fails))
	}
	outcomes := map[string]bool{}
	for _, f := range fails {
		outcomes[f.Reason] = true
		if f.Attrs[TokenCountAttr] == nil {
			t.Errorf("failure %q missing token_count attr", f.Query)
		}
	}
	for _, want := range []string{"buried", "absent", "no_results"} {
		if !outcomes[want] {
			t.Errorf("failure reasons missing %q: %v", want, outcomes)
		}
	}
}

func TestArtistIntentReport_ZeroEvaluatedRatesAreZero(t *testing.T) {
	var r ArtistIntentReport
	if r.Top1Rate() != 0 || r.TopKRate() != 0 || r.BuriedRate() != 0 || r.AbsentRate() != 0 {
		t.Error("all rates must be 0 when nothing was evaluated (no division by zero)")
	}
}

func TestArtistIntentOutcome_StringAndJSON(t *testing.T) {
	tests := []struct {
		o    ArtistIntentOutcome
		want string
	}{
		{ArtistIntentPass, "pass"},
		{ArtistIntentBuried, "buried"},
		{ArtistIntentBelowK, "below_k"},
		{ArtistIntentAbsent, "absent"},
		{ArtistIntentNoResults, "no_results"},
		{ArtistIntentSkipped, "skipped"},
		{ArtistIntentUnknown, "unknown"},
	}
	for _, tt := range tests {
		if got := tt.o.String(); got != tt.want {
			t.Errorf("String() = %q, want %q", got, tt.want)
		}
		b, err := tt.o.MarshalJSON()
		if err != nil || string(b) != `"`+tt.want+`"` {
			t.Errorf("MarshalJSON = %s (%v), want %q", b, err, tt.want)
		}
	}
}
