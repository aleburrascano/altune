package service

import "testing"

func TestSentinelErrorCodes(t *testing.T) {
	cases := []struct {
		err  interface{ ErrorCode() string }
		want string
	}{
		{ErrTrackNotFound, "catalog.track_not_found"},
		{ErrPlaylistNotFound, "catalog.playlist_not_found"},
		{ErrAudioNotAvailable, "catalog.audio_not_available"},
		{ErrAudioOrphaned, "catalog.audio_orphaned"},
	}
	for _, c := range cases {
		if got := c.err.ErrorCode(); got != c.want {
			t.Errorf("ErrorCode: got %q, want %q", got, c.want)
		}
	}
}
