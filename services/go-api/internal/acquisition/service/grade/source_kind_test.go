package grade

import "testing"

func TestSourceKind_IsOfficial(t *testing.T) {
	cases := map[SourceKind]bool{
		ArtTrack:      true,
		OfficialVideo: true,
		LabelUpload:   true,
		FanUpload:     false,
		UserUpload:    false,
		"":            false,
	}
	for kind, want := range cases {
		if got := kind.IsOfficial(); got != want {
			t.Errorf("%q.IsOfficial() = %v, want %v", kind, got, want)
		}
	}
}
