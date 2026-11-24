package domain_test

import (
	"altune/go-api/internal/catalog"
	shareddomain "altune/go-api/internal/shared/domain"
	"testing"
)

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  *shareddomain.CodedError
		want string
	}{
		{catalog.ErrTrackNotFound, "catalog.track_not_found"},
		{catalog.ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{catalog.ErrAudioNotAvailable, "catalog.audio_not_available"},
		{catalog.ErrAudioOrphaned, "catalog.audio_orphaned"},
		{catalog.ErrCatalogTemporarilyUnavailable, "catalog.temporarily_unavailable"},
	}
	for _, c := range cases {
		if got := c.err.Code().String(); got != c.want {
			t.Errorf("code: got %q, want %q", got, c.want)
		}
	}
}
