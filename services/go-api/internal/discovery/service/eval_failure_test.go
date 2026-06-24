package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestTokenCount(t *testing.T) {
	tests := []struct {
		in   string
		want int
	}{
		{"Humble", 1},               // the single-token hard case
		{"Kendrick Lamar Humble", 3},
		{"  spaced   out  ", 2},
		{"Beyoncé", 1},              // diacritics fold, still one token
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
		{"123", "symbol"}, // no letters
	}
	for _, tt := range tests {
		if got := ScriptClass(tt.in); got != tt.want {
			t.Errorf("ScriptClass(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestPopBandAndHasIdentifier(t *testing.T) {
	mk := func(pop float64, isrc string) domain.SearchResult {
		extras := map[string]any{}
		if isrc != "" {
			extras["isrc"] = isrc
		}
		return domain.SearchResult{Popularity: pop, Extras: extras}
	}
	if got := PopBand(mk(0, "")); got != "none" {
		t.Errorf("pop 0 → %q, want none", got)
	}
	if got := PopBand(mk(15, "")); got != "low" {
		t.Errorf("pop 15 → %q, want low", got)
	}
	if got := PopBand(mk(95, "")); got != "high" {
		t.Errorf("pop 95 → %q, want high", got)
	}
	if HasIdentifier(mk(0, "")) {
		t.Error("no isrc/mbid → HasIdentifier should be false")
	}
	if !HasIdentifier(mk(0, "USRC12345")) {
		t.Error("isrc present → HasIdentifier should be true")
	}
}

func TestSliceFailures(t *testing.T) {
	recs := []FailureRecord{
		{Query: "a", Attrs: map[string]any{TokenCountAttr: 1, PopBandAttr: "low"}},
		{Query: "b", Attrs: map[string]any{TokenCountAttr: 1, PopBandAttr: "low"}},
		{Query: "c", Attrs: map[string]any{TokenCountAttr: 3, PopBandAttr: "high"}},
		{Query: "d", Attrs: map[string]any{}}, // missing keys
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
