package domain_test

import (
	"altune/go-api/internal/library"
	apierror "altune/go-api/internal/shared/apierror"
	"testing"
)

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  *apierror.CodedError
		want string
	}{
		{library.ErrTrackNotFound, "catalog.track_not_found"},
		{library.ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{library.ErrAudioNotAvailable, "catalog.audio_not_available"},
		{library.ErrAudioOrphaned, "catalog.audio_orphaned"},
		{library.ErrCatalogTemporarilyUnavailable, "catalog.temporarily_unavailable"},
	}
	for _, c := range cases {
		if got := c.err.Code().String(); got != c.want {
			t.Errorf("code: got %q, want %q", got, c.want)
		}
	}
}
