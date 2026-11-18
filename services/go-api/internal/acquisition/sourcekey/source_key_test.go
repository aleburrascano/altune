package sourcekey_test

import (
	"altune/go-api/internal/acquisition"
	"context"
	"testing"
)

type answeringSongs []acquisition.YtmSong

func (a answeringSongs) SearchSongs(context.Context, string) ([]acquisition.YtmSong, error) {
	return a, nil
}

func ytMusicWatchURL(t *testing.T, videoID string) string {
	t.Helper()
	song := acquisition.YtmSong{VideoID: videoID, Type: acquisition.VideoTypeATV, Title: "Song", Artists: []string{"Artist"}}
	found, err := acquisition.NewYtmSearchSource(answeringSongs{song}, nil).Find(context.Background(), acquisition.FindRequest{Title: "Song", Artist: "Artist"})
	if err != nil || len(found) != 1 {
		t.Fatalf("found %v err %v, want one candidate", found, err)
	}
	return found[0].URL
}

func TestYouTubeMusicWatchURL_KeysAsTheVideo(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	url := ytMusicWatchURL(t, id)
	if url != "https://music.youtube.com/watch?v="+id {
		t.Fatalf("watch URL for %q = %q", id, url)
	}
	if got := acquisition.SourceKey(url); got != "youtube:"+id {
		t.Fatalf("SourceKey(%q) = %q, want %q", url, got, "youtube:"+id)
	}
}

func TestIsYouTubeVideoID_AcceptsExactlyElevenURLSafeCharacters(t *testing.T) {
	cases := []struct {
		id   string
		want bool
	}{
		{"dQw4w9WgXcQ", true},
		{"a_b-C_d-E_f", true},
		{"", false},
		{"dQw4w9WgXc", false},
		{"dQw4w9WgXcQQ", false},
		{"dQw4w9WgXc!", false},
		{"dQw4w9WgXc\n", false},
	}
	for _, tc := range cases {
		if got := acquisition.IsYouTubeVideoID(tc.id); got != tc.want {
			t.Fatalf("IsYouTubeVideoID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
