package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"testing"
)

func TestSourceKind_IsOfficial(t *testing.T) {
	cases := map[domain.SourceKind]bool{
		domain.ArtTrack:      true,
		domain.OfficialVideo: true,
		domain.LabelUpload:   true,
		domain.FanUpload:     false,
		domain.UserUpload:    false,
		"":                   false,
	}
	for kind, want := range cases {
		if got := kind.IsOfficial(); got != want {
			t.Errorf("%q.IsOfficial() = %v, want %v", kind, got, want)
		}
	}
}
