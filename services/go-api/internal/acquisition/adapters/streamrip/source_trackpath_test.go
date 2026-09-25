package streamrip

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"testing"
)

func TestFind_SoundCloudRejectsSetsAndProfiles(t *testing.T) {
	notTracks := []string{
		"https://soundcloud.com/artist/sets/album",
		"https://soundcloud.com/artist/sets",
		"https://soundcloud.com/artist",
		"https://soundcloud.com/artist/",
		"https://soundcloud.com/",
		"https://soundcloud.com",
		"https://soundcloud.com/artist/tracks",
		"https://soundcloud.com/artist/albums",
		"https://soundcloud.com/artist/likes",
		"https://soundcloud.com/artist//song",
		"https://soundcloud.com/artist/song/extra",
		"https://soundcloud.com/sets/song",
	}
	for _, permalink := range notTracks {
		t.Run(permalink, func(t *testing.T) {
			got, err := NewSource("soundcloud").Find(context.Background(), ports.FindRequest{
				Title:    "Song",
				Identity: identityWith("soundcloud", "999", permalink),
			})
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if len(got) != 0 {
				t.Fatalf("candidates = %+v, want none for %q", got, permalink)
			}
		})
	}
}

func TestFind_SoundCloudAcceptsATrackPermalinkWithTrailingSlash(t *testing.T) {
	got, err := NewSource("soundcloud").Find(context.Background(), ports.FindRequest{
		Title:    "Song",
		Identity: identityWith("soundcloud", "999", "https://soundcloud.com/artist/song/"),
	})
	if err != nil || len(got) != 1 {
		t.Fatalf("Find = %+v, %v, want one candidate", got, err)
	}
}
