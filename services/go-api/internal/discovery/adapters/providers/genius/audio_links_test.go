package genius

import (
	"strings"
	"testing"
)

func TestNormalizeAudioLink(t *testing.T) {
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
			got, ok := normalizeAudioLink(tc.provider, tc.in)
			if ok != (tc.want != "") || got.URL != tc.want {
				t.Errorf("normalizeAudioLink(%q, %q) = %q, %v; want %q", tc.provider, tc.in, got.URL, ok, tc.want)
			}
			if ok && string(got.Provider) != tc.provider {
				t.Errorf("provider = %q, want %q", got.Provider, tc.provider)
			}
		})
	}
}
