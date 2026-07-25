package domain

import "testing"

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
		{"unknown reason", reason("solar_flare"), "Couldn't get this track"},
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
