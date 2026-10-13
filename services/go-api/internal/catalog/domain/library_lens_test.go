package domain

import (
	"altune/go-api/internal/shared/sharedtest"
	"strings"
	"testing"
)

func TestParseLibrarySort(t *testing.T) {
	tests := []struct {
		input   string
		want    LibrarySort
		wantErr bool
	}{
		{"", SortRecent, false},
		{"recent", SortRecent, false},
		{"az", SortAlphabetical, false},
		{"year", SortYear, false},
		{"popularity", SortRecent, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseLibrarySort(tt.input)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Errorf("sort = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLibrarySort_String(t *testing.T) {
	if SortAlphabetical.String() != "az" {
		t.Errorf("SortAlphabetical.String() = %q, want %q", SortAlphabetical.String(), "az")
	}
}

func TestFailureMessage(t *testing.T) {
	reason := func(s string) *string { return &s }

	tests := []struct {
		name   string
		reason *string
		want   string
	}{
		{"nil reason", nil, "Acquisition failed"},
		{"known reason", reason("no_match_found"), "Couldn't find this track"},
		{"known code with detail", reason("no_match_found: all 2 candidates rejected (2 identity)"), "Couldn't find this track"},
		{"storage failure", reason("storage_failed"), "Couldn't save this track"},
		{"unknown reason", reason("solar_flare"), "Couldn't get this track"},
		{"unknown reason with detail", reason("solar_flare: no_match_found"), "Couldn't get this track"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FailureMessage(tt.reason); got != tt.want {
				t.Errorf("FailureMessage() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestTotalDurationSeconds(t *testing.T) {
	seconds := func(f float64) *float64 { return &f }
	tracks := []*Track{
		{DurationSeconds: seconds(90)},
		{DurationSeconds: nil},
		{DurationSeconds: seconds(30.5)},
	}

	if got := TotalDurationSeconds(tracks); got != 120.5 {
		t.Errorf("TotalDurationSeconds() = %v, want 120.5", got)
	}
}

func TestNormalizePage(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		offset    int
		wantLimit int
		wantErr   bool
	}{
		{name: "zero limit becomes the default page", limit: 0, wantLimit: 50},
		{name: "negative limit becomes the default page", limit: -5, wantLimit: 50},
		{name: "limit in range passes through", limit: 30, offset: 10, wantLimit: 30},
		{name: "limit at the cap passes through", limit: 2000, wantLimit: 2000},
		{name: "limit over the cap clamps to the cap", limit: 9000, wantLimit: 2000},
		{name: "negative offset is refused", limit: 30, offset: -1, wantErr: true},
		{name: "negative offset is refused before the limit is clamped", limit: 9000, offset: -3, wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit, err := NormalizePage(c.limit, c.offset)

			if c.wantErr {
				if err == nil {
					t.Fatalf("expected a validation error, got limit = %d", limit)
				}
				sharedtest.AssertValidationError(t, err)
				if !strings.Contains(err.Error(), "offset") {
					t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if limit != c.wantLimit {
				t.Errorf("limit = %d, want %d", limit, c.wantLimit)
			}
		})
	}
}
