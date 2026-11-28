package domain_test

import (
	"altune/go-api/internal/download/internal/adapters/ytmsearch"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"context"
	"strings"
	"testing"

	"pgregory.net/rapid"
)

type answeringSongs []ytmsearch.Song

func (a answeringSongs) SearchSongs(context.Context, string) ([]ytmsearch.Song, error) {
	return a, nil
}

func ytMusicWatchURL(t *testing.T, videoID string) string {
	t.Helper()
	song := ytmsearch.Song{VideoID: videoID, Type: ytmsearch.VideoTypeAudioOnly, Title: "Song", Artists: []string{"Artist"}}
	found, err := ytmsearch.NewSource(answeringSongs{song}, nil).Find(context.Background(), ports.FindRequest{Title: "Song", Artist: "Artist"})
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
	if got := domain.SourceKey(url); got != "youtube:"+id {
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
		if got := domain.IsYouTubeVideoID(tc.id); got != tc.want {
			t.Fatalf("IsYouTubeVideoID(%q) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestSourceKey_CollapsesTheSameYouTubeVideo(t *testing.T) {
	same := []string{
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://m.youtube.com/watch?v=dQw4w9WgXcQ",
		"https://youtube.com/watch?v=dQw4w9WgXcQ&list=RDabc",
		"https://youtu.be/dQw4w9WgXcQ?si=xyz",
		"https://www.youtube.com/embed/dQw4w9WgXcQ",
		"https://www.youtube.com:443/watch?v=dQw4w9WgXcQ",
	}

	want := "youtube:dQw4w9WgXcQ"
	for _, raw := range same {
		if got := domain.SourceKey(raw); got != want {
			t.Errorf("acquisition.SourceKey(%q) = %q, want %q — the same video under two spellings must exclude as one", raw, got, want)
		}
	}
}

func TestSourceKey_DistinguishesDifferentVideos(t *testing.T) {
	a := domain.SourceKey("https://www.youtube.com/watch?v=dQw4w9WgXcQ")
	b := domain.SourceKey("https://www.youtube.com/watch?v=aaaaaaaaaaa")
	if a == b {
		t.Errorf("distinct videos collapsed to %q", a)
	}
}

func TestSourceKey_RefusesAVideoIDFromALookalikeHost(t *testing.T) {
	imposters := []string{
		"https://notyoutube.com/watch?v=aaaaaaaaaaa",
		"https://evilyoutube.com/watch?v=aaaaaaaaaaa",
		"https://x.test/?u=youtube.com/watch?v=aaaaaaaaaaa",
		"https://www.youtube.com@evil.test/watch?v=aaaaaaaaaaa",
	}

	for _, raw := range imposters {
		got := domain.SourceKey(raw)
		if strings.HasPrefix(got, "youtube:") {
			t.Errorf("acquisition.SourceKey(%q) = %q, want a key of its own — a lookalike host must not take a video's identity", raw, got)
		}
	}
}

func TestSourceKey_NormalizesNonYouTubeURLs(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://soundcloud.com/artist/song", "soundcloud.com/artist/song"},
		{"http://www.soundcloud.com/artist/song/", "soundcloud.com/artist/song"},
		{"https://soundcloud.com/Artist/Song?in=x", "soundcloud.com/artist/song"},
		{"yt:master", "yt:master"},
	}
	for _, tt := range tests {
		if got := domain.SourceKey(tt.in); got != tt.want {
			t.Errorf("acquisition.SourceKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSourceKey_CollapsesEveryURLSpellingOfOneUpload(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"youtube", []string{
			"https://www.youtube.com/watch?v=dQw4w9WgXcQ",
			"https://www.youtube.com/shorts/dQw4w9WgXcQ",
			"https://youtube.com/live/dQw4w9WgXcQ?feature=share",
			"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
			"https://youtube-nocookie.com/embed/dQw4w9WgXcQ",
		}, "youtube:dQw4w9WgXcQ"},
		{"soundcloud", []string{
			"https://soundcloud.com/artist/song",
			"https://m.soundcloud.com/artist/song",
			"https://www.soundcloud.com/artist/song/",
		}, "soundcloud.com/artist/song"},
	}
	for _, tt := range tests {
		for _, raw := range tt.in {
			if got := domain.SourceKey(raw); got != tt.want {
				t.Errorf("%s: acquisition.SourceKey(%q) = %q, want %q", tt.name, raw, got, tt.want)
			}
		}
	}
}

func TestSourceKey_CollapsesYouTubeSpellingsWithCaseQueryAndFragment(t *testing.T) {
	same := []string{
		"https://WWW.YOUTUBE.COM/watch?v=dQw4w9WgXcQ",
		"https://WWW.YOUTUBE.COM/shorts/dQw4w9WgXcQ",
		"http://www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ/",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ?si=abc123",
		"https://www.youtube.com/shorts/dQw4w9WgXcQ#comments",
		"https://m.youtube.com/shorts/dQw4w9WgXcQ?feature=share",
		"https://youtube.com/shorts/dQw4w9WgXcQ?si=abc&t=10",
		"https://www.youtube.com/live/dQw4w9WgXcQ",
		"https://www.youtube.com/live/dQw4w9WgXcQ?si=abc&t=120",
		"https://www.youtube.com/live/dQw4w9WgXcQ/",
		"https://youtu.be/dQw4w9WgXcQ?t=42&si=xyz",
		"https://youtu.be/dQw4w9WgXcQ#t=42",
		"https://music.youtube.com/watch?v=dQw4w9WgXcQ&list=RDAMVM&si=x",
		"https://www.youtube.com/watch?list=PLabc&v=dQw4w9WgXcQ&t=5",
		"https://www.youtube.com/watch?v=dQw4w9WgXcQ#t=30",
		"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ?start=10&rel=0",
		"https://WWW.YOUTUBE-NOCOOKIE.COM/embed/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com:443/embed/dQw4w9WgXcQ",
		"http://youtube-nocookie.com/embed/dQw4w9WgXcQ#x",
	}

	want := "youtube:dQw4w9WgXcQ"
	for _, raw := range same {
		if got := domain.SourceKey(raw); got != want {
			t.Errorf("acquisition.SourceKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestSourceKey_KeepsDifferentVideosApartAcrossNewSpellings(t *testing.T) {
	pairs := [][2]string{
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube.com/shorts/aaaaaaaaaaa"},
		{"https://www.youtube.com/live/dQw4w9WgXcQ", "https://www.youtube.com/live/aaaaaaaaaaa"},
		{"https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ", "https://www.youtube-nocookie.com/embed/aaaaaaaaaaa"},
		{"https://www.youtube.com/shorts/dQw4w9WgXcQ", "https://www.youtube.com/shorts/DQW4W9WGXCQ"},
		{"https://m.soundcloud.com/artist/song", "https://m.soundcloud.com/artist/other"},
		{"https://m.soundcloud.com/artist/sets/song", "https://soundcloud.com/artist/song"},
	}
	for _, p := range pairs {
		if a, b := domain.SourceKey(p[0]), domain.SourceKey(p[1]); a == b {
			t.Errorf("acquisition.SourceKey(%q) and acquisition.SourceKey(%q) both = %q, want distinct keys", p[0], p[1], a)
		}
	}
}

func TestSourceKey_RefusesAVideoIDFromALookalikeOfTheNewHostsAndPaths(t *testing.T) {
	imposters := []string{
		"https://youtube.com.evil.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com.evil.com/live/dQw4w9WgXcQ",
		"https://evilyoutube.com/shorts/dQw4w9WgXcQ",
		"https://notyoutube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://youtube-nocookie.com.evil.com/embed/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.evil.com/embed/dQw4w9WgXcQ",
		"https://youtu.be.evil.com/dQw4w9WgXcQ",
		"https://m.youtube.com.evil.com/shorts/dQw4w9WgXcQ",
		"https://music.youtube.com.evil.com/watch?v=dQw4w9WgXcQ",
		"https://www.youtube.com@evil.test/shorts/dQw4w9WgXcQ",
		"https://www.youtube-nocookie.com@evil.test/embed/dQw4w9WgXcQ",
		"https://evil.test/www.youtube.com/shorts/dQw4w9WgXcQ",
		"https://evil.test/shorts/dQw4w9WgXcQ",
		"https://evil.test/embed/dQw4w9WgXcQ?u=https://www.youtube-nocookie.com/embed/dQw4w9WgXcQ",
		"https://xn--youtube-ix0d.com/shorts/dQw4w9WgXcQ",
		"https://yоutube.com/shorts/dQw4w9WgXcQ",
		"https://www.youtube.com.:443@evil.test/live/dQw4w9WgXcQ",
	}
	for _, raw := range imposters {
		if got := domain.SourceKey(raw); strings.HasPrefix(got, "youtube:") {
			t.Errorf("acquisition.SourceKey(%q) = %q, want a key that is not a YouTube video key", raw, got)
		}
	}
}

func TestSourceKey_KeepsALookalikeSoundCloudHostOffTheSoundCloudKey(t *testing.T) {
	want := "soundcloud.com/artist/song"
	imposters := []string{
		"https://m.soundcloud.com.attacker.net/artist/song",
		"https://soundcloud.com.attacker.net/artist/song",
		"https://evilsoundcloud.com/artist/song",
		"https://m.evilsoundcloud.com/artist/song",
		"https://m.soundcloud.com@attacker.net/artist/song",
		"https://attacker.net/soundcloud.com/artist/song",
		"https://attacker.net/m.soundcloud.com/artist/song",
		"https://sоundcloud.com/artist/song",
	}
	for _, raw := range imposters {
		got := domain.SourceKey(raw)
		if got == want || strings.HasPrefix(got, "soundcloud.com/") {
			t.Errorf("acquisition.SourceKey(%q) = %q, want a key that is not a soundcloud.com key", raw, got)
		}
	}
}

func TestSourceKey_CollapsesMobileSoundCloudSpellingsOfOneUpload(t *testing.T) {
	tests := []struct {
		in   []string
		want string
	}{
		{[]string{
			"https://m.soundcloud.com/Artist/Song",
			"https://m.soundcloud.com/artist/song/",
			"http://m.soundcloud.com/artist/song",
			"https://m.soundcloud.com/artist/song?si=abc&utm_source=clipboard",
			"https://SoundCloud.com/ARTIST/SONG",
		}, "soundcloud.com/artist/song"},
		{[]string{
			"https://soundcloud.com/artist/sets/album",
			"https://m.soundcloud.com/artist/sets/album",
			"https://m.soundcloud.com/Artist/Sets/Album/",
			"https://www.soundcloud.com/artist/sets/album?si=x",
		}, "soundcloud.com/artist/sets/album"},
	}
	for _, tt := range tests {
		for _, raw := range tt.in {
			if got := domain.SourceKey(raw); got != tt.want {
				t.Errorf("acquisition.SourceKey(%q) = %q, want %q", raw, got, tt.want)
			}
		}
	}
}

func TestSourceKey_GivesNoSharedYouTubeKeyToEmptyOrMalformedInput(t *testing.T) {
	inputs := []string{
		"",
		"   ",
		"not a url",
		"://",
		"https://",
		"https:///shorts/dQw4w9WgXcQ",
		"https://www.youtube.com/shorts/",
		"https://www.youtube.com/shorts",
		"https://www.youtube.com/live/",
		"https://www.youtube.com/live",
		"https://www.youtube-nocookie.com/embed/",
		"https://www.youtube-nocookie.com/",
		"https://youtu.be/",
		"https://www.youtube.com/@artist/live",
		"https://www.youtube.com/shorts/%00%00",
	}
	for _, raw := range inputs {
		if got := domain.SourceKey(raw); got == "youtube:" || got == "youtube:live" || got == "youtube:shorts" || got == "youtube:embed" {
			t.Errorf("acquisition.SourceKey(%q) = %q, want no YouTube key without a video id", raw, got)
		}
	}
	if got := domain.SourceKey(""); got != "" {
		t.Errorf("acquisition.SourceKey(\"\") = %q, want empty", got)
	}
}

func TestSourceKey_RejectsAYouTubeURLWhoseVideoIDIsNotValid(t *testing.T) {
	inputs := []string{
		"https://www.youtube.com/watch?v=short",
		"https://music.youtube.com/watch?v=aaaaaaaaaaaa",
		"https://music.youtube.com/watch?v=aaaaaaaaaa!",
		"https://youtu.be/short",
		"https://www.youtube.com/shorts/short",
		"https://www.youtube.com/playlist?list=PLaaaaaaaaaaa",
		"https://www.youtube.com/watch",
	}
	for _, raw := range inputs {
		if got := domain.SourceKey(raw); got != "" {
			t.Errorf("acquisition.SourceKey(%q) = %q, want no key: a YouTube URL without a valid video id is not a candidate", raw, got)
		}
	}
}

func TestSourceKey_KeepsTwoDifferentItemsOnAQueryIdentifiedHostApart(t *testing.T) {
	first := domain.SourceKey("https://media.example.org/play?item=4821&utm_source=a")
	second := domain.SourceKey("https://media.example.org/play?item=9034&utm_source=a")

	if first == "" || first == second {
		t.Errorf("keys = %q and %q, want two distinct keys for two different items", first, second)
	}
}

func TestSourceKey_KeepsTwoSchemelessItemsApartByTheirQuery(t *testing.T) {
	first := domain.SourceKey("youtube.com/watch?v=album")
	second := domain.SourceKey("youtube.com/watch?v=single")

	if first == "" || first == second {
		t.Errorf("keys = %q and %q, want two distinct keys even without a scheme", first, second)
	}
}

func TestSourceKey_TreatsTheSameQueryInAnyOrderAsOneItem(t *testing.T) {
	a := domain.SourceKey("https://media.example.org/play?item=4821&disc=2")
	b := domain.SourceKey("https://MEDIA.example.org/play/?disc=2&item=4821")

	if a != b {
		t.Errorf("keys = %q and %q, want one key for one item", a, b)
	}
}

func TestSourceKey_CollapsesAnUppercaseMobileSoundCloudHost(t *testing.T) {
	tests := []struct{ in, want string }{
		{"https://M.SOUNDCLOUD.COM/Artist/Song", "soundcloud.com/artist/song"},
		{"https://M.SoundCloud.com/Artist/Sets/Album/", "soundcloud.com/artist/sets/album"},
	}
	for _, tt := range tests {
		if got := domain.SourceKey(tt.in); got != tt.want {
			t.Errorf("acquisition.SourceKey(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSourceKey_EveryYouTubeFormOfAVideoGivesOneKey(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		id := rapid.StringMatching(`[A-Za-z0-9_-]{11}`).Draw(t, "id")
		want := "youtube:" + id
		for _, form := range []string{
			"https://www.youtube.com/watch?v=" + id,
			"https://youtu.be/" + id,
			"https://m.youtube.com/embed/" + id,
			"https://music.youtube.com/watch?v=" + id + "&list=PL1",
			"  https://youtube.com/shorts/" + id + "  ",
		} {
			if got := domain.SourceKey(form); got != want {
				t.Fatalf("SourceKey(%q) = %q, want %q", form, got, want)
			}
		}
	})
}

func TestSourceKey_IgnoresPaddingTrailingSlashAndHostCase(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		host := rapid.StringMatching(`[a-z]{3,10}\.(com|net|org)`).Filter(func(h string) bool {
			return !strings.Contains(h, "youtu")
		}).Draw(t, "host")
		path := rapid.StringMatching(`(/[a-z0-9]{1,8}){0,3}`).Draw(t, "path")
		base := "https://" + host + path

		want := domain.SourceKey(base)

		for _, variant := range []string{" " + base + " ", "https://" + strings.ToUpper(host) + path + "/", "HTTPS://" + host + path} {
			if got := domain.SourceKey(variant); got != want {
				t.Fatalf("SourceKey(%q) = %q, want %q (the key of %q)", variant, got, want, base)
			}
		}
	})
}
