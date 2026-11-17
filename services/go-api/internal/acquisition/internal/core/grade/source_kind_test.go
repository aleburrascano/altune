package grade_test

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

func TestSourceKind_IsOfficial(t *testing.T) {
	cases := map[acquisition.SourceKind]bool{
		acquisition.ArtTrack:      true,
		acquisition.OfficialVideo: true,
		acquisition.LabelUpload:   true,
		acquisition.FanUpload:     false,
		acquisition.UserUpload:    false,
		"":                        false,
	}
	for kind, want := range cases {
		if got := kind.IsOfficial(); got != want {
			t.Errorf("%q.IsOfficial() = %v, want %v", kind, got, want)
		}
	}
}
