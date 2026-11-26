package audioquality_test

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"testing"
)

func TestSourceKind_IsOfficial(t *testing.T) {
	cases := map[audioquality.SourceKind]bool{
		audioquality.ArtTrack:      true,
		audioquality.OfficialVideo: true,
		audioquality.LabelUpload:   true,
		audioquality.FanUpload:     false,
		audioquality.UserUpload:    false,
		"":                         false,
	}
	for kind, want := range cases {
		if got := kind.IsOfficial(); got != want {
			t.Errorf("%q.IsOfficial() = %v, want %v", kind, got, want)
		}
	}
}
