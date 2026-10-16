package domain

import "testing"

func TestYouTubeMusicWatchURL_KeysAsTheVideo(t *testing.T) {
	const id = "dQw4w9WgXcQ"
	if got := SourceKey(YouTubeMusicWatchURL(id)); got != "youtube:"+id {
		t.Fatalf("SourceKey(YouTubeMusicWatchURL(%q)) = %q, want %q", id, got, "youtube:"+id)
	}
	if got := YouTubeMusicWatchURL(id); got != "https://music.youtube.com/watch?v="+id {
		t.Fatalf("YouTubeMusicWatchURL(%q) = %q", id, got)
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
		if got := IsYouTubeVideoID(tc.id); got != tc.want {
			t.Fatalf("IsYouTubeVideoID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}
