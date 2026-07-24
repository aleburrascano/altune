package eval

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// ---- behavioral corpus error paths --------------------------------------

func TestLoadBehavioralCorpus_MissingFile(t *testing.T) {
	if _, err := LoadBehavioralCorpus(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("want an error for a missing corpus file")
	}
}

func TestLoadBehavioralCorpus_BadJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadBehavioralCorpus(path); err == nil {
		t.Fatal("want a parse error for malformed JSON")
	}
}

type erroringLabelStore struct{}

func (erroringLabelStore) BehavioralLabels(context.Context, time.Time) ([]ports.BehavioralLabel, error) {
	return nil, errors.New("db down")
}

func TestCorpusBuilder_BuildAndMaterializePropagateStoreError(t *testing.T) {
	builder := NewCorpusBuilder(erroringLabelStore{})
	if _, err := builder.Build(context.Background(), time.Unix(0, 0), "x"); err == nil {
		t.Error("Build must propagate the label-store error")
	}
	path := filepath.Join(t.TempDir(), "out.json")
	if err := builder.Materialize(context.Background(), time.Unix(0, 0), "x", path); err == nil {
		t.Error("Materialize must propagate the build error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("no corpus file may be written when the build failed")
	}
}

func TestCorpusBuilder_MaterializeUnwritablePath(t *testing.T) {
	store := fakeLabelStore{}
	builder := NewCorpusBuilder(store)
	// A path whose parent directory does not exist → the write step must error.
	path := filepath.Join(t.TempDir(), "no-such-dir", "corpus.json")
	if err := builder.Materialize(context.Background(), time.Unix(0, 0), "x", path); err == nil {
		t.Error("Materialize must surface the write error")
	}
}

// ---- detail report metrics ----------------------------------------------

func TestDetailReport_MetricsDirections(t *testing.T) {
	r := DetailReport{ContaminationCount: 2, AlbumRecall: 0.9, TrackRecall: 0.8, MetadataCoverage: 0.7}
	m := metricByName(t, r.Metrics())
	if got := m["detail.contamination"]; got.Value != 2 || got.HigherIsBetter {
		t.Errorf("contamination = %+v, want 2 lower-is-better", got)
	}
	for name, want := range map[string]float64{
		"detail.album_recall":      0.9,
		"detail.track_recall":      0.8,
		"detail.metadata_coverage": 0.7,
	} {
		if got := m[name]; got.Value != want || !got.HigherIsBetter {
			t.Errorf("%s = %+v, want %v higher-is-better", name, got, want)
		}
	}
}

func TestRunDetailEval_EmptyGoldens(t *testing.T) {
	rep := RunDetailEval(context.Background(), nil, fakeDetailSvc{})
	if rep.Goldens != 0 || rep.AlbumRecall != 0 || rep.TrackRecall != 0 || rep.MetadataCoverage != 0 {
		t.Errorf("empty run must report zeros, got %+v", rep)
	}
	if len(rep.Failures()) != 0 {
		t.Errorf("empty run must have no failures, got %v", rep.Failures())
	}
}

func TestRunDetailEval_NoAlbumsExcludedFromCoverage(t *testing.T) {
	// A golden whose service returns no albums must not drag metadata coverage
	// toward zero — it is excluded from the coverage average entirely.
	goldens := []DetailGolden{
		{Name: "empty", SeedProvider: "deezer", SeedID: "1"},
	}
	rep := RunDetailEval(context.Background(), goldens, fakeDetailSvc{})
	if rep.MetadataCoverage != 0 {
		t.Errorf("coverage = %v, want 0 (nothing measured)", rep.MetadataCoverage)
	}
	if rep.PerArtist[0].MetadataCoverage != 0 {
		t.Errorf("per-artist coverage = %v, want 0", rep.PerArtist[0].MetadataCoverage)
	}
}

// ---- library eval odds and ends -----------------------------------------

func TestEvalOutcome_StringAndJSON(t *testing.T) {
	tests := []struct {
		o    EvalOutcome
		want string
	}{
		{EvalPass, "pass"},
		{EvalFailWrongTop, "fail_wrong_top"},
		{EvalFailNoResults, "fail_no_results"},
		{EvalSkipped, "skipped"},
		{EvalOutcomeUnknown, "unknown"},
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

func TestQueryMode_QueryForAndLabel(t *testing.T) {
	e := LibraryEntity{Title: "HUMBLE.", Artist: "Kendrick Lamar"}
	if got := QueryExact.queryFor(e); got != "Kendrick Lamar HUMBLE." {
		t.Errorf("exact query = %q", got)
	}
	if got := QueryTitleOnly.queryFor(e); got != "HUMBLE." {
		t.Errorf("title-only query = %q", got)
	}
	if QueryExact.label() != "" || QueryTitleOnly.label() != "hard" {
		t.Errorf("labels = %q/%q, want \"\"/hard", QueryExact.label(), QueryTitleOnly.label())
	}
}

func TestRunLibraryEvalMode_AllSkippedReportsZeroRates(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "One", Artist: ""},
		{Title: "Two", Artist: "   "},
	}
	report := RunLibraryEvalMode(context.Background(), entities, &fakeSearcher{}, 1, 3, QueryTitleOnly, nil)
	if report.Evaluated != 0 || report.Skipped != 2 {
		t.Fatalf("Evaluated/Skipped = %d/%d, want 0/2", report.Evaluated, report.Skipped)
	}
	if report.Top1Rate() != 0 || report.TopKRate() != 0 {
		t.Error("all-skipped run must report 0 rates, not NaN")
	}
	if report.Corpus != "hard" {
		t.Errorf("Corpus = %q, want hard", report.Corpus)
	}
}

// ---- correction helpers --------------------------------------------------

type recordedVocab struct {
	entries []domain.VocabularyEntry
	err     error
}

func (v recordedVocab) FindClosest(context.Context, string, int) ([]domain.VocabularyEntry, error) {
	return v.entries, v.err
}

func TestIsRecognizedTerm(t *testing.T) {
	known := recordedVocab{entries: []domain.VocabularyEntry{{TermNorm: "kendrick"}}}
	if !IsRecognizedTerm(context.Background(), known, "Kendrick") {
		t.Error("exact (normalized) vocab hit must be recognized")
	}
	if IsRecognizedTerm(context.Background(), known, "drake") {
		t.Error("a term the store does not hold exactly must not be recognized")
	}
	if IsRecognizedTerm(context.Background(), known, "†††") {
		t.Error("a symbol-only term normalizes to empty and must not be recognized")
	}
	broken := recordedVocab{err: errors.New("redis down")}
	if IsRecognizedTerm(context.Background(), broken, "kendrick") {
		t.Error("a store error must fail closed (not recognized)")
	}
}

// ---- failure-log helpers -------------------------------------------------

func TestStringifyAttrAndItoa(t *testing.T) {
	records := []FailureRecord{
		{Attrs: map[string]any{"k": "str"}},
		{Attrs: map[string]any{"k": true}},
		{Attrs: map[string]any{"k": false}},
		{Attrs: map[string]any{"k": -42}},
		{Attrs: map[string]any{"k": 3.14}}, // unsupported type → "?"
		{Attrs: map[string]any{}},          // missing key → "(unset)"
	}
	got := SliceFailures(records, "k")
	want := map[string]int{"str": 1, "true": 1, "false": 1, "-42": 1, "?": 1, "(unset)": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("slice[%q] = %d, want %d (full: %v)", k, got[k], n, got)
		}
	}
	if itoa(0) != "0" {
		t.Errorf("itoa(0) = %q", itoa(0))
	}
}

func TestStringExtra(t *testing.T) {
	r := domain.SearchResult{Extras: map[string]any{"record_type": "ep", "n": 3}}
	if got := stringExtra(r, "record_type"); got != "ep" {
		t.Errorf("stringExtra = %q, want ep", got)
	}
	if got := stringExtra(r, "n"); got != "" {
		t.Errorf("non-string extra must read as empty, got %q", got)
	}
	if got := stringExtra(domain.SearchResult{}, "record_type"); got != "" {
		t.Errorf("nil extras must read as empty, got %q", got)
	}
}

// ---- neighborRune determinism -------------------------------------------

func TestNeighborRune(t *testing.T) {
	if neighborRune('a') != 's' {
		t.Errorf("neighborRune('a') = %q, want 's' (QWERTY-adjacent)", neighborRune('a'))
	}
	// Off-map lowercase letters shift by one; 'z' is ON the map (→'x').
	if neighborRune('z') != 'x' {
		t.Errorf("neighborRune('z') = %q, want 'x'", neighborRune('z'))
	}
	// Non-letter falls back to 'a'.
	if neighborRune('7') != 'a' {
		t.Errorf("neighborRune('7') = %q, want 'a'", neighborRune('7'))
	}
}
