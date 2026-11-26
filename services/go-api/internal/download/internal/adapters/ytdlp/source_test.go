package ytdlp_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

func findRequest() download.FindRequest {
	return download.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		ISRC:   "USUG11904206",
	}
}

func findRequestSpecs() []string {
	var specs []string
	for _, query := range []string{"USUG11904206", "Blinding Lights The Weeknd", "Blinding Lights The Weeknd After Hours", "Blinding Lights The Weeknd audio"} {
		specs = append(specs, "ytsearch5:"+query, "scsearch5:"+query)
	}
	slices.Sort(specs)
	return specs
}

func findWithYtDlp(t *testing.T, s *download.YouTubeAudioSearcher) ([]download.AudioCandidate, error) {
	t.Helper()
	return download.NewYtDlpSource(s).Find(context.Background(), findRequest())
}

func find(t *testing.T) ([]download.AudioCandidate, error) {
	t.Helper()
	return findWithYtDlp(t, download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep))
}

func TestSource_Find_RunsEveryQueryEvenAfterEnoughCandidatesAreFound(t *testing.T) {
	calls := installSearch(t, "emit 5")

	got, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if specs := searchedSpecs(t, calls); !slices.Equal(specs, findRequestSpecs()) {
		t.Fatalf("searches = %v, want all of %v even though the first query's engines already return 5 candidates", specs, findRequestSpecs())
	}
	if len(got) != 8*5 {
		t.Fatalf("merged candidates = %d, want %d", len(got), 8*5)
	}
}

func TestSource_Find_QueryFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := captureLogs(t)

	if _, err := findWithYtDlp(t, download.MustNewYtDlpAudioSearcher("", secretCookiePath, "", time.Now, clock.Sleep)); err == nil {
		t.Fatal("every query failed, Find reported no error")
	}

	logged := loggedErrors(t, logs, "acquisition.search_query_failed")
	if len(logged) != 4 {
		t.Fatalf("query failure logs = %q, want one per query", logged)
	}
	for _, line := range logged {
		if strings.Contains(line, "/secret") {
			t.Fatalf("the cookie jar path leaked into the log: %s", line)
		}
		if !strings.Contains(line, "no such file or directory") {
			t.Fatalf("redaction dropped the diagnostic text: %s", line)
		}
	}
}

func TestSource_Find_RunsEveryQueryWhileCandidatesStayScarce(t *testing.T) {
	calls := installSearch(t, "emit 1")

	got, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if specs := searchedSpecs(t, calls); !slices.Equal(specs, findRequestSpecs()) {
		t.Fatalf("searches = %v, want all of %v when no query is fruitful", specs, findRequestSpecs())
	}
	if len(got) != 8 {
		t.Fatalf("merged candidates = %d, want 8", len(got))
	}
}

func TestSource_Find_MustHold9_ISRCQueryWithManyHitsStillRunsEveryOtherQueryVariant(t *testing.T) {
	calls := installSearch(t, "emit 1", searchAnswer{"*USUG11904206", "emit 10"})

	got, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if specs := searchedSpecs(t, calls); !slices.Equal(specs, findRequestSpecs()) {
		t.Fatalf("searches = %v, want all of %v: the ISRC hit count must never suppress another query variant", specs, findRequestSpecs())
	}
	if want := 10*2 + 3*2; len(got) != want {
		t.Fatalf("merged candidates = %d, want %d (the ISRC hits on both engines plus one per other query/engine pair)", len(got), want)
	}
}

func concurrencyMeasuringSearch(t *testing.T) string {
	t.Helper()
	running, counts := t.TempDir(), filepath.Join(t.TempDir(), "counts")
	installSearch(t, "touch '"+running+"/'$$; ls '"+running+"' | wc -l >> '"+counts+"'; sleep 0.3; rm -f '"+running+"/'$$; emit 1")
	return counts
}

func mostSearchesSeenAtOnce(t *testing.T, counts string) int {
	t.Helper()
	raw, err := os.ReadFile(counts)
	if err != nil {
		t.Fatal(err)
	}
	most := 0
	for line := range strings.Lines(string(raw)) {
		seen, err := strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			t.Fatalf("count line %q: %v", line, err)
		}
		most = max(most, seen)
	}
	return most
}

func TestSource_Find_NeverRunsMoreThanFourSearchesAtOnce(t *testing.T) {
	counts := concurrencyMeasuringSearch(t)

	if _, err := find(t); err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if most := mostSearchesSeenAtOnce(t, counts); most != 4 {
		t.Fatalf("max concurrent searches observed = %d, want the bound of 4 reached and never passed", most)
	}
}

func TestSource_Find_OrderIsDeterministicAndSurvivesOneFailingSearch(t *testing.T) {
	installSearch(t, "emit 1", searchAnswer{"scsearch5:*audio*", "fail 'soundcloud blew up'"})

	first, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	second, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(first) != 7 {
		t.Fatalf("Find returned %d candidates, want 7 with only one of eight searches failing", len(first))
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Find order changed across runs with identical fake responses:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestSource_Find_ThreeOfFourQueriesFailingStillMergesTheSurvivor(t *testing.T) {
	installSearch(t, "fail 'provider blew up'", searchAnswer{`*"Blinding Lights The Weeknd audio"`, "emit 1"})

	got, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v, want nil since one query variant still answered", err)
	}
	if len(got) != 2 {
		t.Fatalf("merged candidates = %d, want 2 (only the surviving query's engines)", len(got))
	}
}

func TestSource_Find_EveryQueryUnavailableIsSourceUnavailable(t *testing.T) {
	installSearch(t, "fail 'ERROR: HTTP Error 429: Too Many Requests'")

	_, err := find(t)

	if !isSourceUnavailable(err) {
		t.Fatalf("Find error = %v, want a source-unavailable error when every query and engine is unavailable", err)
	}
}

func TestSource_Find_NoQueryFindsAnythingReturnsEmptyNotError(t *testing.T) {
	installSearch(t, "true")

	got, err := find(t)
	if err != nil {
		t.Fatalf("Find error: %v, want nil when every search simply found nothing", err)
	}
	if len(got) != 0 {
		t.Fatalf("merged candidates = %d, want 0", len(got))
	}
}

func soundCloudOnlySearch(flatDuration float64, inspect string) string {
	flat := fmt.Sprintf(`{"title":"Drinking in L.A.","webpage_url":"%s","duration":%g}`, soundCloudTrackURL, flatDuration)
	return ytDlpByMode(searchAnswers("true", searchAnswer{"scsearch5:*", "echo '" + flat + "'"}), inspect, "")
}

func findUnplayable(t *testing.T, flatDuration float64, inspect string) string {
	t.Helper()
	installYtDlp(t, soundCloudOnlySearch(flatDuration, inspect))
	got, err := download.NewYtDlpSource(download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep)).
		Find(context.Background(), download.FindRequest{Title: "Drinking in L.A.", Artist: "Bran Van 3000"})
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want the one soundcloud track", got)
	}
	return got[0].Unplayable
}

func inspected(json string) string {
	return "echo '" + json + "'"
}

func TestSource_Find_MustHold8_SoundCloudTrackWithOnlyEncryptedFormatsIsMarkedDRM(t *testing.T) {
	inspect := inspected(`{"duration":240,"formats":[{"format_id":"hls_aac_160k","vcodec":"none","has_drm":true},{"format_id":"hls_opus_64k","vcodec":"none","protocol":"m3u8_native_encrypted"}]}`)

	if got := findUnplayable(t, 240, inspect); got != "drm" {
		t.Fatalf("Unplayable = %q, want drm", got)
	}
}

func TestSource_Find_SoundCloudTrackWithAClearFormatStaysPlayable(t *testing.T) {
	inspect := inspected(`{"duration":240,"formats":[{"format_id":"hls_aac_160k_encrypted","vcodec":"none","has_drm":true},{"format_id":"http_mp3_0_0","vcodec":"none","has_drm":false}]}`)

	if got := findUnplayable(t, 240, inspect); got != "" {
		t.Fatalf("Unplayable = %q, want playable", got)
	}
}

func TestSource_Find_SoundCloudThirtySecondExtractOfALongerTrackIsMarkedPreview(t *testing.T) {
	if got := findUnplayable(t, 240, inspected(`{"duration":30}`)); got != "preview" {
		t.Fatalf("Unplayable = %q, want preview", got)
	}
}

func TestSource_Find_SoundCloudPreviewFormatURLIsMarkedPreview(t *testing.T) {
	inspect := inspected(`{"duration":240,"formats":[{"format_id":"http_mp3_1_0","vcodec":"none","url":"https://cf-media.sndcdn.com/preview/abc.mp3"}]}`)

	if got := findUnplayable(t, 240, inspect); got != "preview" {
		t.Fatalf("Unplayable = %q, want preview", got)
	}
}

func TestSource_Find_TrackThatIsReallyThirtySecondsStaysPlayable(t *testing.T) {
	if got := findUnplayable(t, 30, inspected(`{"duration":30}`)); got != "" {
		t.Fatalf("Unplayable = %q, want playable", got)
	}
}

func TestSource_Find_PreviewDurationBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		extract    string
		flat       float64
		unplayable string
	}{
		{"extract at the slack edge is a preview", "30.5", 240, "preview"},
		{"extract past the slack edge is playable", "30.6", 240, ""},
		{"flat duration at the margin is playable", "30", 35, ""},
		{"flat duration past the margin is a preview", "30", 35.1, "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := findUnplayable(t, tc.flat, inspected(`{"duration":`+tc.extract+`}`)); got != tc.unplayable {
				t.Fatalf("Unplayable = %q, want %q", got, tc.unplayable)
			}
		})
	}
}

func TestSource_Find_InspectionFailureLeavesTheCandidatePlayableAndIsLogged(t *testing.T) {
	logs := captureLogs(t)

	if got := findUnplayable(t, 240, "fail 'boom'"); got != "" {
		t.Fatalf("Unplayable = %q, want fail-open", got)
	}
	if !strings.Contains(logs.String(), "acquisition.soundcloud_inspect_failed") {
		t.Fatalf("inspection failure not logged:\n%s", logs.String())
	}
}

func inspectedURLs(t *testing.T, calls string) []string {
	t.Helper()
	var urls []string
	for _, argv := range recordedCalls(t, calls) {
		if slices.Contains(argv, "-J") {
			urls = append(urls, argv[len(argv)-1])
		}
	}
	return urls
}

func TestSource_Find_InspectsOnlySoundCloudURLsAndOncePerURL(t *testing.T) {
	bothSources := `printf '%s\n%s\n' '{"title":"yt","webpage_url":"https://www.youtube.com/watch?v=aaaaaaaaaaa","duration":240}' '{"title":"sc","webpage_url":"` + soundCloudTrackURL + `","duration":240}'`
	calls := installYtDlp(t, ytDlpByMode(bothSources, inspected(`{"duration":240}`), ""))
	src := download.NewYtDlpSource(download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep))

	for range 2 {
		if _, err := src.Find(context.Background(), download.FindRequest{Title: "Drinking in L.A.", Artist: "Bran Van 3000"}); err != nil {
			t.Fatalf("Find error: %v", err)
		}
	}

	if urls := inspectedURLs(t, calls); !slices.Equal(urls, []string{soundCloudTrackURL}) {
		t.Fatalf("inspected = %v, want only the soundcloud url, once", urls)
	}
}

func soundCloudTracks(from, to int) []download.AudioCandidate {
	var tracks []download.AudioCandidate
	for i := from; i <= to; i++ {
		tracks = append(tracks, download.AudioCandidate{URL: fmt.Sprintf("https://soundcloud.com/a/track-%d", i), Duration: 240})
	}
	return tracks
}

func TestMarkUnplayable_RemembersTheLatest500InspectionsAndForgetsTheOldest(t *testing.T) {
	calls := installYtDlp(t, inspectedAs(`{"duration":240}`))
	s := download.MustNewYtDlpAudioSearcher("", "", "", time.Now, clock.Sleep)
	mark := func(tracks []download.AudioCandidate) { s.MarkUnplayable(context.Background(), tracks) }

	mark(soundCloudTracks(0, 0))
	mark(soundCloudTracks(1, 500))
	mark(soundCloudTracks(1, 500))
	if got := len(inspectedURLs(t, calls)); got != 501 {
		t.Fatalf("inspections = %d, want 501: the newest 500 were all still remembered", got)
	}

	mark(soundCloudTracks(0, 0))
	if got := len(inspectedURLs(t, calls)); got != 502 {
		t.Fatalf("inspections = %d, want 502: the oldest was forgotten past the bound", got)
	}
}

func TestMarkUnplayable_ReinspectsAURLOnceItsDayIsUp(t *testing.T) {
	calls := installYtDlp(t, inspectedAs(`{"duration":240}`))
	inspectedAt := time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)
	now := inspectedAt
	s := download.MustNewYtDlpAudioSearcher("", "", "", func() time.Time { return now }, clock.Sleep)
	inspectionsAt := func(at time.Time) int {
		now = at
		s.MarkUnplayable(context.Background(), soundCloudTracks(0, 0))
		return len(inspectedURLs(t, calls))
	}

	inspectionsAt(inspectedAt)
	if got := inspectionsAt(inspectedAt.Add(24*time.Hour - time.Nanosecond)); got != 1 {
		t.Fatalf("inspections = %d, want the first still served just before 24h", got)
	}
	if got := inspectionsAt(inspectedAt.Add(24*time.Hour + time.Nanosecond)); got != 2 {
		t.Fatalf("inspections = %d, want a fresh inspection once 24h have passed", got)
	}
}

func TestNewYtDlpAudioSearcher_RefusesAMissingClockOrSleeper(t *testing.T) {
	if s, err := download.NewYtDlpAudioSearcher("", "", "", nil, clock.Sleep); s != nil || !errors.Is(err, download.ErrSearcherClockRequired) {
		t.Errorf("nil clock: got (%v, %v), want ErrSearcherClockRequired", s, err)
	}
	if s, err := download.NewYtDlpAudioSearcher("", "", "", time.Now, nil); s != nil || !errors.Is(err, download.ErrSearcherSleeperRequired) {
		t.Errorf("nil sleeper: got (%v, %v), want ErrSearcherSleeperRequired", s, err)
	}
}
