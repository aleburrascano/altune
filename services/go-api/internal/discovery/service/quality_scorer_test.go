package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestComputeQualityScore(t *testing.T) {
	tests := []struct {
		name             string
		result           domain.SearchResult
		fetchSuccessRate float64
		wantMinComposite float64
		wantMaxComposite float64
	}{
		{
			name: "full completeness 4/4 fields, single source, no tier",
			result: domain.SearchResult{
				ImageURL: "https://img.example.com/art.jpg",
				Sources:  []domain.SourceRef{{Provider: domain.ProviderDeezer}},
				Extras: map[string]any{
					"isrc":             "USRC17607839",
					"duration_seconds": 240,
					"album":            "OK Computer",
				},
			},
			fetchSuccessRate: 1.0,
			// completeness=1.0, agreement=1/6, tier=0.2 (none), fetch=1.0 => (1.0+0.167+0.2+1.0)/4 ≈ 0.59
			wantMinComposite: 0.50,
			wantMaxComposite: 0.70,
		},
		{
			name: "no completeness fields, no sources",
			result: domain.SearchResult{
				Sources: nil,
				Extras:  nil,
			},
			fetchSuccessRate: 0.0,
			// completeness=0, agreement=0, tier=0.2 (none), fetch=0 => 0.2/4 = 0.05
			wantMinComposite: 0.04,
			wantMaxComposite: 0.06,
		},
		{
			name: "mixed completeness 2/4, two sources, isrc tier",
			result: domain.SearchResult{
				ImageURL: "https://img.example.com/art.jpg",
				Sources: []domain.SourceRef{
					{Provider: domain.ProviderDeezer},
					{Provider: domain.ProviderMusicBrainz},
				},
				Extras: map[string]any{
					"isrc":            "USRC17607839",
					"resolution_tier": "isrc",
				},
			},
			fetchSuccessRate: 0.5,
			// completeness=2/4=0.5, agreement=2/6≈0.33, tier=0.8 (isrc), fetch=0.5 => (0.5+0.33+0.8+0.5)/4 ≈ 0.53
			wantMinComposite: 0.45,
			wantMaxComposite: 0.60,
		},
		{
			name: "fetch success rate clamped to 0",
			result: domain.SearchResult{
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer}},
				Extras:  map[string]any{},
			},
			fetchSuccessRate: -1.0,
			wantMinComposite: 0.0,
			wantMaxComposite: 0.15,
		},
		{
			name: "fetch success rate clamped to 1",
			result: domain.SearchResult{
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer}},
				Extras:  map[string]any{},
			},
			fetchSuccessRate: 5.0,
			// clamped to 1.0
			wantMinComposite: 0.25,
			wantMaxComposite: 0.40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := ComputeQualityScore(tt.result, tt.fetchSuccessRate)
			if score.Completeness < tt.wantMinComposite || score.Completeness > tt.wantMaxComposite {
				t.Errorf("ComputeQualityScore().Completeness = %.4f, want [%.2f, %.2f]",
					score.Completeness, tt.wantMinComposite, tt.wantMaxComposite)
			}
		})
	}
}

func TestComputeQualityScore_AgreementField(t *testing.T) {
	// Agreement should be len(sources)/6, capped at 1.0
	tests := []struct {
		name      string
		nSources  int
		wantAgrmt float64
	}{
		{"zero sources", 0, 0.0},
		{"one source", 1, 1.0 / 6.0},
		{"three sources", 3, 3.0 / 6.0},
		{"six sources", 6, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sources := make([]domain.SourceRef, tt.nSources)
			for i := range sources {
				sources[i] = domain.SourceRef{Provider: domain.ProviderName(i)}
			}
			r := domain.SearchResult{Sources: sources}
			score := ComputeQualityScore(r, 0.0)
			diff := score.Agreement - tt.wantAgrmt
			if diff < -0.001 || diff > 0.001 {
				t.Errorf("Agreement = %.4f, want %.4f", score.Agreement, tt.wantAgrmt)
			}
		})
	}
}

func TestIsDemoted(t *testing.T) {
	tests := []struct {
		name       string
		recordType string // empty means no record_type extra
		want       bool
	}{
		{name: "album not demoted", recordType: "album", want: false},
		{name: "single not demoted", recordType: "single", want: false},
		{name: "ep not demoted", recordType: "ep", want: false},
		{name: "no record_type not demoted", recordType: "", want: false},
		{name: "compilation demoted", recordType: "compilation", want: true},
		{name: "live demoted", recordType: "live", want: true},
		{name: "remix demoted", recordType: "remix", want: true},
		{name: "Album case insensitive not demoted", recordType: "Album", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			extras := map[string]any{}
			if tt.recordType != "" {
				extras["record_type"] = tt.recordType
			}
			r := domain.SearchResult{Extras: extras}
			got := IsDemoted(r)
			if got != tt.want {
				t.Errorf("IsDemoted(record_type=%q) = %v, want %v", tt.recordType, got, tt.want)
			}
		})
	}
}
