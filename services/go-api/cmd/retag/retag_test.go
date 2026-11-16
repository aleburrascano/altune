package main

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/catalog"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

const ref = "user/Slayr/Album/Song.m4a"

func fakeFile(tags acquisition.TrackTags, audio string) []byte {
	raw, _ := json.Marshal(tags)
	return append(append(raw, '\n'), audio...)
}

func splitFakeFile(content []byte) (acquisition.TrackTags, string) {
	head, audio, _ := bytes.Cut(content, []byte("\n"))
	var tags acquisition.TrackTags
	_ = json.Unmarshal(head, &tags)
	return tags, string(audio)
}

type fakeMedia struct{}

func (fakeMedia) ReadTags(_ context.Context, path string) (acquisition.TrackTags, error) {
	content, err := os.ReadFile(path)
	tags, _ := splitFakeFile(content)
	return tags, err
}

func (fakeMedia) AudioDigest(_ context.Context, path string) (string, error) {
	content, err := os.ReadFile(path)
	_, audio := splitFakeFile(content)
	return audio, err
}

type fakeTagger struct {
	mangleAudio bool
	skipWriting bool
	err         error
}

func (f fakeTagger) Tag(_ context.Context, path string, tags acquisition.TrackTags) error {
	if f.err != nil {
		return f.err
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	old, audio := splitFakeFile(content)
	if f.skipWriting {
		tags = old
	}
	if f.mangleAudio {
		audio += "-mangled"
	}
	return os.WriteFile(path, fakeFile(tags, audio), 0o600)
}

type recordingStore struct {
	mu          sync.Mutex
	files       map[string][]byte
	errOnStore  error
	errOnDelete error
	ops         []string
	onStore     func(ref string)
}

func newRecordingStore() *recordingStore {
	return &recordingStore{files: map[string][]byte{}}
}

func (s *recordingStore) Seed(target string, data []byte) {
	s.files[target] = data
}

func (s *recordingStore) Exists(_ context.Context, target string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.files[target]
	return ok, nil
}

func (s *recordingStore) Store(_ context.Context, source, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onStore != nil {
		s.onStore(target)
	}
	s.ops = append(s.ops, "store "+target)
	if s.errOnStore != nil {
		return s.errOnStore
	}
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	s.files[target] = content
	return nil
}

func (s *recordingStore) Stream(_ context.Context, target string) (catalog.AudioStream, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, ok := s.files[target]
	if !ok {
		return nil, 0, &fs.PathError{Op: "open", Path: target, Err: fs.ErrNotExist}
	}
	return audioStream{bytes.NewReader(data)}, int64(len(data)), nil
}

type audioStream struct{ *bytes.Reader }

func (audioStream) Close() error { return nil }

func (s *recordingStore) Delete(_ context.Context, target string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ops = append(s.ops, "delete "+target)
	if s.errOnDelete != nil {
		return s.errOnDelete
	}
	delete(s.files, target)
	return nil
}

var _ catalog.AudioStore = (*recordingStore)(nil)

var staleTags = with(fullTags, func(x *acquisition.TrackTags) { x.Title = "Old Title"; x.Year = 0 })

func newRetagger(store *recordingStore, tagger fakeTagger) retagger {
	return retagger{
		store:       store,
		tagger:      tagger,
		media:       fakeMedia{},
		attemptID:   func() string { return "attempt" },
		stillServes: func(context.Context, trackRow) (bool, error) { return true, nil },
		claim:       newClaim(0),
	}
}

func seeded(tags acquisition.TrackTags) *recordingStore {
	store := newRecordingStore()
	store.Seed(ref, fakeFile(tags, "AUDIO"))
	return store
}

var track = trackRow{ID: "t1", UserID: "user", Ref: ref, Tags: fullTags}

func TestProcess_SkipsATrackWhoseTagsAlreadyMatch(t *testing.T) {
	store := seeded(fullTags)

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, true)

	if got.Verdict != verdictMatched || len(store.ops) != 0 {
		t.Fatalf("verdict %s, ops %v; want matched with no writes", got.Verdict, store.ops)
	}
}

func TestProcess_DryRunReportsTheDiffAndWritesNothing(t *testing.T) {
	store := seeded(staleTags)
	before := string(store.files[ref])

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, false)

	if got.Verdict != verdictWouldRetag || len(got.Diffs) != 2 {
		t.Fatalf("verdict %s, diffs %+v", got.Verdict, got.Diffs)
	}
	if len(store.ops) != 0 || string(store.files[ref]) != before {
		t.Fatalf("dry run wrote: ops %v", store.ops)
	}
}

func TestProcess_StagesThenReplacesThenDeletesTheStagedObject(t *testing.T) {
	store := seeded(staleTags)
	var presentWhenReplacing []string
	store.onStore = func(target string) {
		if target == ref {
			for key := range store.files {
				presentWhenReplacing = append(presentWhenReplacing, key)
			}
		}
	}

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, true)

	wantOps := []string{"store user/Slayr/Album/Song.retag-attempt.m4a", "store " + ref, "delete user/Slayr/Album/Song.retag-attempt.m4a"}
	if got.Verdict != verdictRetagged || !slices.Equal(store.ops, wantOps) {
		t.Fatalf("verdict %s err %v ops %v; want %v", got.Verdict, got.Err, store.ops, wantOps)
	}
	if !slices.Contains(presentWhenReplacing, ref) || !slices.Contains(presentWhenReplacing, "user/Slayr/Album/Song.retag-attempt.m4a") {
		t.Errorf("objects present when replacing = %v, want the original and the staged copy", presentWhenReplacing)
	}
	tags, audio := splitFakeFile(store.files[ref])
	if tags != fullTags || audio != "AUDIO" {
		t.Errorf("stored file = %+v / %q, want database tags and the original audio", tags, audio)
	}
	if len(store.files) != 1 {
		t.Errorf("objects left = %d, want only the canonical key", len(store.files))
	}
}

func TestProcess_RunningTwiceSecondRunFindsNothingToDo(t *testing.T) {
	store := seeded(staleTags)
	r := newRetagger(store, fakeTagger{})
	r.process(context.Background(), track, true)

	got := r.process(context.Background(), track, true)

	if got.Verdict != verdictMatched {
		t.Fatalf("second run verdict = %s, want matched", got.Verdict)
	}
}

func TestProcess_RefusesToUploadWhenTheAudioStreamChanged(t *testing.T) {
	store := seeded(staleTags)
	before := string(store.files[ref])

	got := newRetagger(store, fakeTagger{mangleAudio: true}).process(context.Background(), track, true)

	if got.Verdict != verdictFailed || !errors.Is(got.Err, errAudioChanged) {
		t.Fatalf("verdict %s err %v, want failed with errAudioChanged", got.Verdict, got.Err)
	}
	if len(store.ops) != 0 || string(store.files[ref]) != before {
		t.Fatalf("upload happened: ops %v", store.ops)
	}
}

func TestProcess_RefusesToUploadWhenTheTaggerLeftTagsWrong(t *testing.T) {
	store := seeded(staleTags)

	got := newRetagger(store, fakeTagger{skipWriting: true}).process(context.Background(), track, true)

	if got.Verdict != verdictFailed || !errors.Is(got.Err, errTagsNotWritten) || len(store.ops) != 0 {
		t.Fatalf("verdict %s err %v ops %v", got.Verdict, got.Err, store.ops)
	}
}

func TestProcess_TaggerFailureLeavesTheObjectAlone(t *testing.T) {
	store := seeded(staleTags)

	got := newRetagger(store, fakeTagger{err: errors.New("boom")}).process(context.Background(), track, true)

	if got.Verdict != verdictFailed || len(store.ops) != 0 {
		t.Fatalf("verdict %s ops %v", got.Verdict, store.ops)
	}
}

func TestProcess_FailedStagingLeavesTheCanonicalObjectUntouched(t *testing.T) {
	store := seeded(staleTags)
	before := string(store.files[ref])
	store.errOnStore = errors.New("bucket unavailable")

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, true)

	if got.Verdict != verdictFailed || string(store.files[ref]) != before || len(store.files) != 1 {
		t.Fatalf("verdict %s, objects %d", got.Verdict, len(store.files))
	}
}

func TestProcess_DoesNotReplaceATrackThatNoLongerServesTheKey(t *testing.T) {
	store := seeded(staleTags)
	before := string(store.files[ref])
	r := newRetagger(store, fakeTagger{})
	r.stillServes = func(context.Context, trackRow) (bool, error) { return false, nil }

	got := r.process(context.Background(), track, true)

	if got.Verdict != verdictMoved || string(store.files[ref]) != before || len(store.files) != 1 {
		t.Fatalf("verdict %s, objects %d", got.Verdict, len(store.files))
	}
}

func TestProcess_ReportsAStagedObjectItCouldNotDelete(t *testing.T) {
	store := seeded(staleTags)
	store.errOnDelete = errors.New("delete denied")

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, true)

	if got.Verdict != verdictRetagged || got.Leftover != "user/Slayr/Album/Song.retag-attempt.m4a" {
		t.Fatalf("verdict %s leftover %q", got.Verdict, got.Leftover)
	}
}

func TestProcess_MissingObjectFails(t *testing.T) {
	store := newRecordingStore()

	got := newRetagger(store, fakeTagger{}).process(context.Background(), track, true)

	if got.Verdict != verdictFailed {
		t.Fatalf("verdict %s", got.Verdict)
	}
}

func TestProcess_LimitStopsBeforeTheSecondChange(t *testing.T) {
	store := seeded(staleTags)
	r := newRetagger(store, fakeTagger{})
	r.claim = newClaim(1)

	first := r.process(context.Background(), track, false)
	second := r.process(context.Background(), track, false)

	if first.Verdict != verdictWouldRetag || second.Verdict != verdictLimitReached {
		t.Fatalf("verdicts %s, %s", first.Verdict, second.Verdict)
	}
}

func TestStagedRefKeepsTheExtensionAndDirectory(t *testing.T) {
	if got := stagedRef("u/a/b/t.m4a", "x"); got != "u/a/b/t.retag-x.m4a" {
		t.Fatalf("stagedRef = %q", got)
	}
}

type fixedTracks []trackRow

func (f fixedTracks) readyTracks(context.Context, selection) ([]trackRow, error) { return f, nil }

func TestRunRetag_ReportsTallyAndSkipsSharedKeys(t *testing.T) {
	store := newRecordingStore()
	var tracks fixedTracks
	for i := range 6 {
		key := fmt.Sprintf("user/A/B/%d.m4a", i)
		tags := staleTags
		if i%2 == 0 {
			tags = fullTags
		}
		store.Seed(key, fakeFile(tags, "AUDIO"))
		tracks = append(tracks, trackRow{ID: fmt.Sprint(i), UserID: "user", Ref: key, Tags: fullTags})
	}
	tracks = append(tracks, trackRow{ID: "dup1", Ref: "user/A/B/0.m4a", Tags: staleTags}, trackRow{ID: "dup2", Ref: "user/A/B/0.m4a", Tags: fullTags})
	var served atomic.Int32
	r := newRetagger(store, fakeTagger{})
	r.stillServes = func(context.Context, trackRow) (bool, error) { served.Add(1); return true, nil }
	var out strings.Builder

	err := runRetag(context.Background(), tracks, r, selection{}, true, 3, &out)
	if err != nil {
		t.Fatalf("runRetag = %v\n%s", err, out.String())
	}
	for _, want := range []string{"examined:      8", "already match: 2", "retagged:      3", "shared-ref:    3", "failed:        0"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("report missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunRetag_ErrorsWhenATrackFails(t *testing.T) {
	store := newRecordingStore()
	var out strings.Builder

	err := runRetag(context.Background(), fixedTracks{track}, newRetagger(store, fakeTagger{}), selection{}, false, 1, &out)

	if err == nil || !strings.Contains(out.String(), "failed") {
		t.Fatalf("err %v\n%s", err, out.String())
	}
}
