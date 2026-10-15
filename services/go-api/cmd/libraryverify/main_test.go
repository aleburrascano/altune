package main

import (
	"altune/go-api/internal/acquisition/adapters/chromaprint"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
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

func TestPreviewSettingsFromEnv_DefaultsAreConservativeForDeezer(t *testing.T) {
	got, err := previewSettingsFromEnv(envOf(nil))

	if err != nil || got.deezerRate > 2 || got.deezerRate <= 0 || got.cut.matchMax >= got.cut.absentMin {
		t.Errorf("settings = %+v, %v; want a deezer rate in (0, 2] and a match cut below the absent cut", got, err)
	}
}

func TestPreviewSettingsFromEnv_ReadsOverridesAndRefusesBadOnes(t *testing.T) {
	got, err := previewSettingsFromEnv(envOf(map[string]string{deezerRateVarName: "5", matchMaxVarName: "0.2", absentMinVarName: "0.45"}))
	if err != nil || got != (previewSettings{deezerRate: 5, cut: previewCut{matchMax: 0.2, absentMin: 0.45}}) {
		t.Errorf("settings = %+v, %v; want the overrides", got, err)
	}
	tests := []struct {
		name string
		env  map[string]string
		want error
	}{
		{"deezer above 5 per second", map[string]string{deezerRateVarName: "6"}, errBadDeezerRate},
		{"deezer zero rate", map[string]string{deezerRateVarName: "0"}, errBadDeezerRate},
		{"cut not a number", map[string]string{matchMaxVarName: "low"}, errBadPreviewCut},
		{"cut of 1", map[string]string{absentMinVarName: "1"}, errBadPreviewCut},
		{"match cut at the absent cut", map[string]string{matchMaxVarName: "0.4", absentMinVarName: "0.4"}, errBadPreviewCut},
		{"match cut above the default absent cut", map[string]string{matchMaxVarName: "0.45"}, errBadPreviewCut},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := previewSettingsFromEnv(envOf(tt.env)); !errors.Is(err, tt.want) {
				t.Errorf("err = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestParseOptions_ReadsTheTracksFileAndCalibrateFlags(t *testing.T) {
	got, err := parseOptions([]string{"-tracks", "/x/ids.txt", "-calibrate"}, io.Discard)

	if err != nil || got.tracksFile != "/x/ids.txt" || !got.calibrate {
		t.Errorf("got %+v, %v", got, err)
	}
}

func TestReadTrackIDs_SplitsOnWhitespaceAndRefusesAnEmptyList(t *testing.T) {
	dir := t.TempDir()
	listed := filepath.Join(dir, "ids.txt")
	blank := filepath.Join(dir, "blank.txt")
	if err := os.WriteFile(listed, []byte("a\nb\n\n c \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blank, []byte(" \n"), 0o600); err != nil {
		t.Fatal(err)
	}

	ids, err := readTrackIDs(listed)
	if err != nil || !slices.Equal(ids, []string{"a", "b", "c"}) {
		t.Errorf("ids = %v, %v; want a b c", ids, err)
	}
	if _, err := readTrackIDs(blank); !errors.Is(err, errEmptyTracksFile) {
		t.Errorf("blank err = %v, want errEmptyTracksFile", err)
	}
	if ids, err := readTrackIDs(""); err != nil || ids != nil {
		t.Errorf("no file: ids = %v, err = %v; want every track", ids, err)
	}
}
