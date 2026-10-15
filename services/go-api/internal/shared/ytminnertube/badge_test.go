package ytminnertube

import "testing"

func badge(icon string) map[string]any {
	return map[string]any{"musicInlineBadgeRenderer": map[string]any{"icon": map[string]any{"iconType": icon}}}
}

func TestHasExplicitBadge_ScansEveryBadge(t *testing.T) {
	cases := []struct {
		name   string
		badges any
		want   bool
	}{
		{"first", []any{badge("MUSIC_EXPLICIT_BADGE")}, true},
		{"second after another badge", []any{badge("MUSIC_LIVE_BADGE"), badge("MUSIC_EXPLICIT_BADGE")}, true},
		{"other badge only", []any{badge("MUSIC_LIVE_BADGE")}, false},
		{"empty list", []any{}, false},
		{"nil", nil, false},
		{"non-list", map[string]any{"a": "b"}, false},
		{"malformed entries", []any{"x", nil, map[string]any{"musicInlineBadgeRenderer": "x"}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HasExplicitBadge(tc.badges); got != tc.want {
				t.Fatalf("HasExplicitBadge = %v, want %v", got, tc.want)
			}
		})
	}
}
