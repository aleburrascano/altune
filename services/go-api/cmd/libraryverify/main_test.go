package main

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"errors"
	"io"
	"testing"
)

func envOf(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestSettingsFromEnv_DefaultsRespectTheProvidersLimits(t *testing.T) {
	got, err := settingsFromEnv(envOf(nil))
	if err != nil {
		t.Fatalf("settingsFromEnv: %v", err)
	}
	want := settings{concurrency: 3, acoustIDRate: 3, musicBrainzRate: 1, minScore: chromaprint.MinScore}
	if got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
}

func TestSettingsFromEnv_ReadsOverrides(t *testing.T) {
	got, err := settingsFromEnv(envOf(map[string]string{
		concurrencyVarName: "8", acoustIDRateVarName: "1.5", musicBrainzRateVarName: "0.5", minScoreVarName: "0.9",
	}))
	if err != nil {
		t.Fatalf("settingsFromEnv: %v", err)
	}
	want := settings{concurrency: 8, acoustIDRate: 1.5, musicBrainzRate: 0.5, minScore: 0.9}
	if got != want {
		t.Errorf("settings = %+v, want %+v", got, want)
	}
}

func TestSettingsFromEnv_RejectsValuesThatBreakAProvidersLimit(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{"zero concurrency", map[string]string{concurrencyVarName: "0"}, errBadConcurrency},
		{"too much concurrency", map[string]string{concurrencyVarName: "17"}, errBadConcurrency},
		{"concurrency not a number", map[string]string{concurrencyVarName: "many"}, errBadConcurrency},
		{"acoustid above 3 per second", map[string]string{acoustIDRateVarName: "4"}, errBadAcoustIDRate},
		{"acoustid zero rate", map[string]string{acoustIDRateVarName: "0"}, errBadAcoustIDRate},
		{"musicbrainz above 1 per second", map[string]string{musicBrainzRateVarName: "2"}, errBadMusicBrainzRate},
		{"score below what the app binds at", map[string]string{minScoreVarName: "0.8"}, errBadMinScore},
		{"score above 1", map[string]string{minScoreVarName: "1.1"}, errBadMinScore},
		{"score not a number", map[string]string{minScoreVarName: "high"}, errBadMinScore},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := settingsFromEnv(envOf(tt.env)); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParseOptions_DefaultsAndFlags(t *testing.T) {
	got, err := parseOptions([]string{"-user", "u-1", "-limit", "5", "-report", "/x/r.json"}, io.Discard)

	if err != nil || got != (options{userID: "u-1", limit: 5, reportPath: "/x/r.json"}) {
		t.Errorf("got %+v, %v", got, err)
	}
	defaults, err := parseOptions(nil, io.Discard)
	if err != nil || defaults.reportPath != defaultReportPath || defaults.limit != 0 {
		t.Errorf("defaults = %+v, %v", defaults, err)
	}
}

func TestParseOptions_RejectsANegativeLimit(t *testing.T) {
	if _, err := parseOptions([]string{"-limit", "-1"}, io.Discard); !errors.Is(err, errNegativeLimit) {
		t.Errorf("err = %v, want errNegativeLimit", err)
	}
}
