package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"strings"
	"testing"
)

import (
	"errors"
	"reflect"
	"sync"
	"time"
)

func findRequest() ports.FindRequest {
	return ports.FindRequest{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		ISRC:   "USUG11904206",
	}
}

func candidatesPerSpec(spec string, n int) []ports.AudioCandidate {
	out := make([]ports.AudioCandidate, 0, n)
	for i := range n {
		out = append(out, ports.AudioCandidate{
			Title: spec,
			URL:   fmt.Sprintf("https://example.test/%s/%d", spec, i),
		})
	}
	return out
}

func TestSource_Find_RunsEveryQueryEvenAfterEnoughCandidatesAreFound(t *testing.T) {
	var mu sync.Mutex
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		return candidatesPerSpec(spec, 5), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantSearches := len(ports.SearchQueries(findRequest())) * len(searchEngines)
	if len(specs) != wantSearches {
		t.Fatalf("searches = %d %v, want all %d even though the first query's engines already return %d candidates",
			len(specs), specs, wantSearches, ports.EnoughCandidates)
	}
	if len(got) != wantSearches*5 {
		t.Fatalf("merged candidates = %d, want %d", len(got), wantSearches*5)
	}
}

func TestSource_Find_QueryFailureLogRedactsTheCookiePath(t *testing.T) {
	logs := captureLogs(t)
	src := NewSource(withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, cookieJarError()
	}))

	if _, err := src.Find(context.Background(), findRequest()); err == nil {
		t.Fatal("every query failed, Find reported no error")
	}

	logged := logs.String()
	if !strings.Contains(logged, "acquisition.search_query_failed") {
		t.Fatalf("expected the query failure log, got:\n%s", logged)
	}
	if strings.Contains(logged, "/secret") {
		t.Fatalf("the cookie jar path leaked into the log:\n%s", logged)
	}
	if !strings.Contains(logged, "exit status 1") {
		t.Fatalf("redaction dropped the diagnostic text:\n%s", logged)
	}
}

func TestSource_Find_RunsEveryQueryWhileCandidatesStayScarce(t *testing.T) {
	var mu sync.Mutex
	var specs []string
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		specs = append(specs, spec)
		mu.Unlock()
		return candidatesPerSpec(spec, 1), nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantSearches := len(ports.SearchQueries(findRequest())) * len(searchEngines)
	if len(specs) != wantSearches {
		t.Fatalf("searches = %d %v, want all %d when no query is fruitful", len(specs), specs, wantSearches)
	}
	if len(got) != wantSearches {
		t.Fatalf("merged candidates = %d, want %d", len(got), wantSearches)
	}
}

func concurrencySafeRunner(run func(spec string) ([]ports.AudioCandidate, error)) searchRunner {
	var mu sync.Mutex
	return func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		defer mu.Unlock()
		return run(spec)
	}
}

func TestSource_Find_MustHold9_ISRCQueryWithManyHitsStillRunsEveryOtherQueryVariant(t *testing.T) {
	req := findRequest()
	seen := map[string]bool{}
	src := NewSource(withRunner(concurrencySafeRunner(func(spec string) ([]ports.AudioCandidate, error) {
		seen[spec] = true
		if strings.Contains(spec, req.ISRC) {
			return candidatesPerSpec(spec, 10), nil
		}
		return candidatesPerSpec(spec, 1), nil
	})))

	got, err := src.Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	wantQueries := ports.SearchQueries(req)
	if len(wantQueries) < 4 {
		t.Fatalf("findRequest fixture carries only %d query variants, want at least 4 to prove this", len(wantQueries))
	}
	for _, query := range wantQueries {
		for _, engine := range searchEngines {
			if !seen[engine+query] {
				t.Fatalf("spec %q never ran, want the ISRC hit count to never suppress another query variant", engine+query)
			}
		}
	}

	wantCandidates := 10*len(searchEngines) + (len(wantQueries)-1)*len(searchEngines)
	if len(got) != wantCandidates {
		t.Fatalf("merged candidates = %d, want %d (the ISRC hits on both engines plus one per other query/engine pair)", len(got), wantCandidates)
	}
}

func TestSource_Find_NeverRunsMoreThanFourSearchesAtOnce(t *testing.T) {
	var mu sync.Mutex
	current, maxSeen := 0, 0
	src := NewSource(withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		mu.Lock()
		current++
		if current > maxSeen {
			maxSeen = current
		}
		mu.Unlock()

		time.Sleep(20 * time.Millisecond)

		mu.Lock()
		current--
		mu.Unlock()

		return candidatesPerSpec(spec, 1), nil
	}))

	if _, err := src.Find(context.Background(), findRequest()); err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if maxSeen > maxConcurrentSearches {
		t.Fatalf("max concurrent searches observed = %d, want no more than %d", maxSeen, maxConcurrentSearches)
	}
	if maxSeen < maxConcurrentSearches {
		t.Fatalf("max concurrent searches observed = %d, want the bound of %d to actually be reached", maxSeen, maxConcurrentSearches)
	}
}

func TestSource_Find_OrderIsDeterministicAndSurvivesOneFailingSearch(t *testing.T) {
	newSrc := func() *Source {
		return NewSource(withRunner(concurrencySafeRunner(func(spec string) ([]ports.AudioCandidate, error) {
			if strings.Contains(spec, "scsearch5:") && strings.Contains(spec, "audio") {
				return nil, errors.New("soundcloud blew up")
			}
			return candidatesPerSpec(spec, 1), nil
		})))
	}

	first, err := newSrc().Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	second, err := newSrc().Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}

	if len(first) == 0 {
		t.Fatal("Find returned no candidates despite only one of eight searches failing")
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("Find order changed across runs with identical fake responses:\nfirst:  %+v\nsecond: %+v", first, second)
	}
}

func TestSource_Find_ThreeOfFourQueriesFailingStillMergesTheSurvivor(t *testing.T) {
	req := findRequest()
	survivorQuery := ports.SearchQueries(req)[len(ports.SearchQueries(req))-1]
	src := NewSource(withRunner(concurrencySafeRunner(func(spec string) ([]ports.AudioCandidate, error) {
		if strings.Contains(spec, survivorQuery) {
			return candidatesPerSpec(spec, 1), nil
		}
		return nil, errors.New("provider blew up")
	})))

	got, err := src.Find(context.Background(), req)
	if err != nil {
		t.Fatalf("Find error: %v, want nil since one query variant still answered", err)
	}
	if len(got) != len(searchEngines) {
		t.Fatalf("merged candidates = %d, want %d (only the surviving query's engines)", len(got), len(searchEngines))
	}
}

func TestSource_Find_EveryQueryUnavailableIsSourceUnavailable(t *testing.T) {
	src := NewSource(withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, &ports.SourceUnavailableError{Source: SourceName, Err: errors.New("throttled")}
	}))

	_, err := src.Find(context.Background(), findRequest())

	if !ports.IsSourceUnavailable(err) {
		t.Fatalf("Find error = %v, want a source-unavailable error when every query and engine is unavailable", err)
	}
}

func TestSource_Find_NoQueryFindsAnythingReturnsEmptyNotError(t *testing.T) {
	src := NewSource(withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return nil, nil
	}))

	got, err := src.Find(context.Background(), findRequest())
	if err != nil {
		t.Fatalf("Find error: %v, want nil when every search simply found nothing", err)
	}
	if len(got) != 0 {
		t.Fatalf("merged candidates = %d, want 0", len(got))
	}
}

const drinkingInLA = "https://soundcloud.com/bran-van-3000/drinking-in-l-a-3"

func soundCloudFindRequest() ports.FindRequest {
	return ports.FindRequest{Title: "Drinking in L.A.", Artist: "Bran Van 3000"}
}

func soundCloudSearcher(flat ports.AudioCandidate, inspect inspectRunner) *YtDlpAudioSearcher {
	s := withRunner(func(_ context.Context, spec string) ([]ports.AudioCandidate, error) {
		if strings.HasPrefix(spec, "scsearch5:") {
			return []ports.AudioCandidate{flat}, nil
		}
		return nil, nil
	})
	s.inspect = inspect
	return s
}

func findUnplayable(t *testing.T, s *YtDlpAudioSearcher) string {
	t.Helper()
	got, err := NewSource(s).Find(context.Background(), soundCloudFindRequest())
	if err != nil {
		t.Fatalf("Find error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("candidates = %v, want the one soundcloud track", got)
	}
	return got[0].Unplayable
}

func staticInspection(result inspection) inspectRunner {
	return func(context.Context, string) (inspection, error) { return result, nil }
}

func TestSource_Find_MustHold8_SoundCloudTrackWithOnlyEncryptedFormatsIsMarkedDRM(t *testing.T) {
	info := inspectionFromInfo(inspectedInfo{Duration: 240, Formats: []inspectedFormat{
		{FormatID: "hls_aac_160k_encrypted", VCodec: "none", HasDRM: true},
		{FormatID: "hls_opus_64k", VCodec: "none", Protocol: "m3u8_native_encrypted"},
	}})
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240}, staticInspection(info))

	if got := findUnplayable(t, s); got != "drm" {
		t.Fatalf("Unplayable = %q, want drm", got)
	}
}

func TestSource_Find_SoundCloudTrackWithAClearFormatStaysPlayable(t *testing.T) {
	info := inspectionFromInfo(inspectedInfo{Duration: 240, Formats: []inspectedFormat{
		{FormatID: "hls_aac_160k_encrypted", VCodec: "none", HasDRM: true},
		{FormatID: "http_mp3_0_0", VCodec: "none", HasDRM: false},
	}})
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240}, staticInspection(info))

	if got := findUnplayable(t, s); got != "" {
		t.Fatalf("Unplayable = %q, want playable", got)
	}
}

func TestSource_Find_SoundCloudThirtySecondExtractOfALongerTrackIsMarkedPreview(t *testing.T) {
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240},
		staticInspection(inspection{Duration: 30}))

	if got := findUnplayable(t, s); got != "preview" {
		t.Fatalf("Unplayable = %q, want preview", got)
	}
}

func TestSource_Find_SoundCloudPreviewFormatURLIsMarkedPreview(t *testing.T) {
	info := inspectionFromInfo(inspectedInfo{Duration: 240, Formats: []inspectedFormat{
		{FormatID: "http_mp3_1_0", VCodec: "none", URL: "https://cf-media.sndcdn.com/preview/abc.mp3"},
	}})
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240}, staticInspection(info))

	if got := findUnplayable(t, s); got != "preview" {
		t.Fatalf("Unplayable = %q, want preview", got)
	}
}

func TestSource_Find_TrackThatIsReallyThirtySecondsStaysPlayable(t *testing.T) {
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 30},
		staticInspection(inspection{Duration: 30}))

	if got := findUnplayable(t, s); got != "" {
		t.Fatalf("Unplayable = %q, want playable", got)
	}
}

func TestSource_Find_PreviewDurationBoundaries(t *testing.T) {
	cases := []struct {
		name       string
		extract    float64
		flat       float64
		unplayable string
	}{
		{"extract at the slack edge is a preview", 30.5, 240, "preview"},
		{"extract past the slack edge is playable", 30.6, 240, ""},
		{"flat duration at the margin is playable", 30, 35, ""},
		{"flat duration past the margin is a preview", 30, 35.1, "preview"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: tc.flat},
				staticInspection(inspection{Duration: tc.extract}))

			if got := findUnplayable(t, s); got != tc.unplayable {
				t.Fatalf("Unplayable = %q, want %q", got, tc.unplayable)
			}
		})
	}
}

func TestSource_Find_InspectionFailureLeavesTheCandidatePlayableAndIsLogged(t *testing.T) {
	logs := captureLogs(t)
	s := soundCloudSearcher(ports.AudioCandidate{Title: "Drinking in L.A.", URL: drinkingInLA, Duration: 240},
		func(context.Context, string) (inspection, error) { return inspection{}, errors.New("boom") })

	if got := findUnplayable(t, s); got != "" {
		t.Fatalf("Unplayable = %q, want fail-open", got)
	}
	if !strings.Contains(logs.String(), "acquisition.soundcloud_inspect_failed") {
		t.Fatalf("inspection failure not logged:\n%s", logs.String())
	}
}

func TestSource_Find_InspectsOnlySoundCloudURLsAndOncePerURL(t *testing.T) {
	var mu sync.Mutex
	inspected := map[string]int{}
	s := withRunner(func(context.Context, string) ([]ports.AudioCandidate, error) {
		return []ports.AudioCandidate{
			{Title: "yt", URL: "https://www.youtube.com/watch?v=aaaaaaaaaaa", Duration: 240},
			{Title: "sc", URL: drinkingInLA, Duration: 240},
		}, nil
	})
	s.inspect = func(_ context.Context, u string) (inspection, error) {
		mu.Lock()
		inspected[u]++
		mu.Unlock()
		return inspection{Duration: 240}, nil
	}
	src := NewSource(s)

	for range 2 {
		if _, err := src.Find(context.Background(), soundCloudFindRequest()); err != nil {
			t.Fatalf("Find error: %v", err)
		}
	}

	if len(inspected) != 1 || inspected[drinkingInLA] != 1 {
		t.Fatalf("inspected = %v, want only the soundcloud url, once", inspected)
	}
}

func TestInspectionCache_ExpiresAfterADayAndEvictsOldestBeyondTheBound(t *testing.T) {
	cache := newInspectionCache()
	now := time.Now()
	cache.now = func() time.Time { return now }

	for i := range inspectCacheMax + 1 {
		cache.put(fmt.Sprintf("u%d", i), inspection{Duration: 1})
	}
	if _, ok := cache.get("u0"); ok {
		t.Fatal("oldest entry survived past the bound")
	}
	if _, ok := cache.get("u1"); !ok {
		t.Fatal("entry within the bound was evicted")
	}
	now = now.Add(inspectCacheTTL + time.Second)
	if _, ok := cache.get("u1"); ok {
		t.Fatal("entry served after its 24h expiry")
	}
}
