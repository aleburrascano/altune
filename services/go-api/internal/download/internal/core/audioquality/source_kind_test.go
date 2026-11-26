package audioquality_test

import (
	"altune/go-api/internal/download"
	"testing"
)

func TestSourceKind_IsOfficial(t *testing.T) {
	cases := map[download.SourceKind]bool{
		download.ArtTrack:      true,
		download.OfficialVideo: true,
		download.LabelUpload:   true,
		download.FanUpload:     false,
		download.UserUpload:    false,
		"":                     false,
	}
	for kind, want := range cases {
		if got := kind.IsOfficial(); got != want {
			t.Errorf("%q.IsOfficial() = %v, want %v", kind, got, want)
		}
	}
}
