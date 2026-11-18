package genius_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSongDetail_NormalisesEachAudioLink(t *testing.T) {
	long := "https://soundcloud.com/a/" + strings.Repeat("x", 200)
	tests := []struct {
		name, provider, in, want string
	}{
		{"soundcloud strips query", "soundcloud", "https://soundcloud.com/a/b?in=x", "https://soundcloud.com/a/b"},
		{"soundcloud strips fragment", "soundcloud", "https://soundcloud.com/a/b#t=1", "https://soundcloud.com/a/b"},
		{"soundcloud http upgraded", "soundcloud", "http://soundcloud.com/a/b", "https://soundcloud.com/a/b"},
		{"soundcloud subdomain", "soundcloud", "https://m.soundcloud.com/a/b", "https://m.soundcloud.com/a/b"},
		{"soundcloud foreign host", "soundcloud", "https://evilsoundcloud.com/a/b", ""},
		{"soundcloud lookalike suffix", "soundcloud", "https://soundcloud.com.evil.io/a", ""},
		{"youtube watch", "youtube", "https://www.youtube.com/watch?v=dQw4w9WgXcQ", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtube extra params", "youtube", "http://youtube.com/watch?feature=x&v=dQw4w9WgXcQ&t=9", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtu.be", "youtube", "https://youtu.be/dQw4w9WgXcQ?t=5", "https://www.youtube.com/watch?v=dQw4w9WgXcQ"},
		{"youtube bad id", "youtube", "https://www.youtube.com/watch?v=short", ""},
		{"youtube no id", "youtube", "https://www.youtube.com/watch", ""},
		{"youtube foreign host", "youtube", "https://example.com/watch?v=dQw4w9WgXcQ", ""},
		{"unknown provider", "spotify", "https://open.spotify.com/track/1", ""},
		{"unparsable", "soundcloud", "http://[::1", ""},
		{"non web scheme", "soundcloud", "ftp://soundcloud.com/a", ""},
		{"too long", "soundcloud", long, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			media, err := json.Marshal([]map[string]string{{"provider": tc.provider, "url": tc.in}})
			if err != nil {
				t.Fatal(err)
			}
			catalog := songCatalog(t, map[string]string{
				"/songs/1": `{"response":{"song":{"media":` + string(media) + `}}}`,
			})

			detail, err := catalog.SongDetail(context.Background(), 1)
			if err != nil {
				t.Fatalf("SongDetail: %v", err)
			}
			if tc.want == "" {
				if len(detail.AudioLinks) != 0 {
					t.Errorf("AudioLinks = %+v, want none for %q", detail.AudioLinks, tc.in)
				}
				return
			}
			if len(detail.AudioLinks) != 1 || detail.AudioLinks[0].URL != tc.want {
				t.Fatalf("AudioLinks = %+v, want one link %q", detail.AudioLinks, tc.want)
			}
			if got := string(detail.AudioLinks[0].Provider); got != tc.provider {
				t.Errorf("provider = %q, want %q", got, tc.provider)
			}
		})
	}
}
