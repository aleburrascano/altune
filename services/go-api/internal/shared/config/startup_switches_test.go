package config

import "testing"

// TestLoad_AcquisitionPaused guards the ACQUISITION_PAUSED startup setting
// (#2800): it defaults to false and an operator can pause acquisition from
// the environment, without an /admin POST.
func TestLoad_AcquisitionPaused(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{name: "default", env: "", want: false},
		{name: "paused", env: "true", want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(nil))
			if tc.env != "" {
				t.Setenv("ACQUISITION_PAUSED", tc.env)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.AcquisitionPaused != tc.want {
				t.Fatalf("AcquisitionPaused = %v, want %v", cfg.AcquisitionPaused, tc.want)
			}
		})
	}
}

// TestLoad_DisabledJobs guards the DISABLED_JOBS startup setting (#2800): it
// splits on commas, trims whitespace around each name, and an unset or empty
// value leaves no jobs disabled.
func TestLoad_DisabledJobs(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		want []string
	}{
		{name: "unset", set: false, want: nil},
		{name: "empty", env: "", set: true, want: nil},
		{name: "trims whitespace", env: "eval meter, stream recovery", set: true, want: []string{"eval meter", "stream recovery"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setEnv(t, validConfigEnv(nil))
			if tc.set {
				t.Setenv("DISABLED_JOBS", tc.env)
			}

			cfg, err := Load()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(cfg.DisabledJobs) != len(tc.want) {
				t.Fatalf("DisabledJobs = %v, want %v", cfg.DisabledJobs, tc.want)
			}
			for i, name := range tc.want {
				if cfg.DisabledJobs[i] != name {
					t.Fatalf("DisabledJobs = %v, want %v", cfg.DisabledJobs, tc.want)
				}
			}
		})
	}
}
