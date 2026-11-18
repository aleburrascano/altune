package domain_test

import (
	"altune/go-api/internal/catalog"
	"testing"
)

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  interface{ ErrorCode() string }
		want string
	}{
		{catalog.ErrTrackNotFound, "catalog.track_not_found"},
		{catalog.ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{catalog.ErrAudioNotAvailable, "catalog.audio_not_available"},
		{catalog.ErrAudioOrphaned, "catalog.audio_orphaned"},
		{catalog.ErrCatalogTemporarilyUnavailable, "catalog.temporarily_unavailable"},
	}
	for _, c := range cases {
		if got := c.err.ErrorCode(); got != c.want {
			t.Errorf("ErrorCode: got %q, want %q", got, c.want)
		}
	}
}
