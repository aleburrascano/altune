package eval

import (
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
		if got := TokenCount(tt.in); got != tt.want {
			t.Errorf("TokenCount(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestScriptClass(t *testing.T) {
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
		if got := ScriptClass(tt.in); got != tt.want {
			t.Errorf("ScriptClass(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSliceFailures(t *testing.T) {
	recs := []FailureRecord{
		{Query: "a", Attrs: map[string]any{TokenCountAttr: 1, PopBandAttr: "low"}},
		{Query: "b", Attrs: map[string]any{TokenCountAttr: 1, PopBandAttr: "low"}},
		{Query: "c", Attrs: map[string]any{TokenCountAttr: 3, PopBandAttr: "high"}},
		{Query: "d", Attrs: map[string]any{}},
	}
	byTok := SliceFailures(recs, TokenCountAttr)
	if byTok["1"] != 2 || byTok["3"] != 1 || byTok["(unset)"] != 1 {
		t.Errorf("byToken slice wrong: %v", byTok)
	}
	pair := SliceFailuresByPair(recs, TokenCountAttr, PopBandAttr)
	if pair["1|low"] != 2 {
		t.Errorf("pair slice wrong: %v", pair)
	}
	top := TopBuckets(byTok, 2)
	if len(top) != 2 || top[0] != "1=2" {
		t.Errorf("TopBuckets = %v, want [1=2 ...]", top)
	}
}

func TestStringifyAttr(t *testing.T) {
	records := []FailureRecord{
		{Attrs: map[string]any{"k": "str"}},
		{Attrs: map[string]any{"k": true}},
		{Attrs: map[string]any{"k": false}},
		{Attrs: map[string]any{"k": -42}},
		{Attrs: map[string]any{"k": 3.14}},
		{Attrs: map[string]any{}},
	}
	got := SliceFailures(records, "k")
	want := map[string]int{"str": 1, "true": 1, "false": 1, "-42": 1, "?": 1, "(unset)": 1}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("slice[%q] = %d, want %d (full: %v)", k, got[k], n, got)
		}
	}
}
