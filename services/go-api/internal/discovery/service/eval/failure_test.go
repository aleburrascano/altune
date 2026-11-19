package eval_test

import (
	"altune/go-api/internal/discovery"
	"testing"
)

func TestTokenCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"Humble", 1},
		{"Kendrick Lamar Humble", 3},
		{"  spaced   out  ", 2},
		{"Beyoncé", 1},
		{"", 0},
	}
	for _, tt := range tests {
		if got := discovery.TokenCount(tt.in); got != tt.want {
			t.Errorf("TokenCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func scriptOf(t *testing.T, query string) any {
	t.Helper()
	report := discovery.EvalReport{Results: []discovery.EvalResult{{Query: query, Outcome: discovery.EvalFailNoResults}}}
	failures := report.Failures()
	if len(failures) != 1 {
		t.Fatalf("got %d failures, want 1", len(failures))
	}
	return failures[0].Attrs[discovery.ScriptAttr]
}

func TestFailures_TagEachQueryWithItsScript(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"Drake", "latin"},
		{"中島美嘉", "nonlatin"},
		{"¥$", "symbol"},
		{"BTS 방탄소년단", "mixed"},
		{"123", "symbol"},
	}
	for _, tt := range tests {
		if got := scriptOf(t, tt.in); got != tt.want {
			t.Errorf("script of %q = %v, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSliceFailures(t *testing.T) {
	recs := []discovery.FailureRecord{
		{Query: "a", Attrs: map[string]any{discovery.TokenCountAttr: 1, discovery.PopBandAttr: "low"}},
		{Query: "b", Attrs: map[string]any{discovery.TokenCountAttr: 1, discovery.PopBandAttr: "low"}},
		{Query: "c", Attrs: map[string]any{discovery.TokenCountAttr: 3, discovery.PopBandAttr: "high"}},
		{Query: "d", Attrs: map[string]any{}},
	}
	byTok := discovery.SliceFailures(recs, discovery.TokenCountAttr)
	if byTok["1"] != 2 || byTok["3"] != 1 || byTok["(unset)"] != 1 {
		t.Errorf("byToken slice wrong: %v", byTok)
	}
	pair := discovery.SliceFailuresByPair(recs, discovery.TokenCountAttr, discovery.PopBandAttr)
	if pair["1|low"] != 2 {
		t.Errorf("pair slice wrong: %v", pair)
	}
	top := discovery.TopBuckets(byTok, 2)
	if len(top) != 2 || top[0] != "1=2" {
		t.Errorf("TopBuckets = %v, want [1=2 ...]", top)
	}
}

func TestStringifyAttr(t *testing.T) {
	records := []discovery.FailureRecord{
		{Attrs: map[string]any{"k": "str"}},
		{Attrs: map[string]any{"k": true}},
		{Attrs: map[string]any{"k": false}},
		{Attrs: map[string]any{"k": -42}},
		{Attrs: map[string]any{"k": 3.14}},
		{Attrs: map[string]any{}},
	}
	got := discovery.SliceFailures(records, "k")
	want := map[string]int{"str": 1, "true": 1, "false": 1, "-42": 1, "?": 1, "(unset)": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("slice[%q] = %d, want %d (full: %v)", k, got[k], n, got)
		}
	}
}
