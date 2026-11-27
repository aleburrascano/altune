package stagingsweep_test

import (
	"altune/go-api/cmd/api/commands/stagingsweep"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/config"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSweepDeletesAnUnreferencedObjectOnceItIsAnHourOld(t *testing.T) {
	cases := []struct {
		name        string
		age         time.Duration
		wantDeleted bool
	}{
		{name: "just uploaded", age: 0, wantDeleted: false},
		{name: "under the hour", age: 59 * time.Minute, wantDeleted: false},
		{name: "a second short of the hour", age: time.Hour - time.Second, wantDeleted: false},
		{name: "exactly one hour", age: time.Hour, wantDeleted: true},
		{name: "well over an hour", age: 3 * time.Hour, wantDeleted: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			const ref = "staging/a.mp3"
			mtime := time.Now().Truncate(time.Second)
			cfg, path := newSweepFixture(t, mtime, ref)

			got := runSweepFixture(t, cfg, &fakeRefLister{}, true, mtime.Add(tc.age))

			want := stagingsweep.Tally{KeptFresh: 1}
			if tc.wantDeleted {
				want = stagingsweep.Tally{Deleted: 1}
			}
			if got != want {
				t.Errorf("Sweep at age %s = %+v, want %+v", tc.age, got, want)
			}
			if present := fileExists(path(ref)); present == tc.wantDeleted {
				t.Errorf("file present = %v at age %s, want %v", present, tc.age, !tc.wantDeleted)
			}
		})
	}
}

type fakeRefLister struct{ refs map[string]bool }

func (f *fakeRefLister) AudioRefsUnderPrefix(context.Context, string) (map[string]bool, error) {
	return f.refs, nil
}

func newSweepFixture(t *testing.T, mtime time.Time, refs ...string) (*config.Config, func(string) string) {
	t.Helper()
	dir := t.TempDir()
	for _, ref := range refs {
		p := filepath.Join(dir, ref)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	return &config.Config{MusicDir: dir, AudioKeyPrefix: "staging/"}, func(ref string) string { return filepath.Join(dir, ref) }
}

func runSweepFixture(t *testing.T, cfg *config.Config, refs *fakeRefLister, execute bool, now time.Time) stagingsweep.Tally {
	t.Helper()
	store, err := library.NewAudioStoreFromConfig(cfg, slog.Default())
	if err != nil {
		t.Fatalf("storage.FromConfig: %v", err)
	}
	lister, ok := store.(library.AudioAgeLister)
	if !ok {
		t.Fatal("store is not an AudioAgeLister")
	}
	run := func() (stagingsweep.Tally, error) {
		return stagingsweep.Preview(context.Background(), lister, refs, now)
	}
	if execute {
		run = func() (stagingsweep.Tally, error) {
			return stagingsweep.Sweep(context.Background(), store, lister, refs, now)
		}
	}
	tally, err := run()
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	return tally
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSweepStagingNoObjects(t *testing.T) {
	cfg, _ := newSweepFixture(t, time.Now())
	got := runSweepFixture(t, cfg, &fakeRefLister{}, true, time.Now())
	if want := (stagingsweep.Tally{}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestSweepStagingKeepsReferencedOldObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	cfg, path := newSweepFixture(t, mtime, ref)
	got := runSweepFixture(t, cfg, &fakeRefLister{refs: map[string]bool{ref: true}}, true, mtime.Add(2*time.Hour))
	if want := (stagingsweep.Tally{KeptReferenced: 1}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !fileExists(path(ref)) {
		t.Error("referenced file was deleted")
	}
}

func TestSweepStagingUnreferencedOldObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	now := mtime.Add(2 * time.Hour)

	t.Run("execute deletes", func(t *testing.T) {
		cfg, path := newSweepFixture(t, mtime, ref)
		got := runSweepFixture(t, cfg, &fakeRefLister{}, true, now)
		if want := (stagingsweep.Tally{Deleted: 1}); got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if fileExists(path(ref)) {
			t.Error("file survived an execute sweep")
		}
	})
	t.Run("dry run keeps the file", func(t *testing.T) {
		cfg, path := newSweepFixture(t, mtime, ref)
		got := runSweepFixture(t, cfg, &fakeRefLister{}, false, now)
		if want := (stagingsweep.Tally{Deleted: 1}); got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
		if !fileExists(path(ref)) {
			t.Error("dry run deleted the file")
		}
	})
}

func TestSweepStagingKeepsFreshUnreferencedObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	cfg, path := newSweepFixture(t, mtime, ref)
	got := runSweepFixture(t, cfg, &fakeRefLister{}, true, mtime.Add(59*time.Minute))
	if want := (stagingsweep.Tally{KeptFresh: 1}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !fileExists(path(ref)) {
		t.Error("fresh file was deleted")
	}
}

func TestSweepStagingLookAlikeRefDoesNotProtectObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	cfg, path := newSweepFixture(t, mtime, ref)
	refs := &fakeRefLister{refs: map[string]bool{"staging/a.mp3.part": true}}
	got := runSweepFixture(t, cfg, refs, true, mtime.Add(2*time.Hour))
	if got.Deleted != 1 || got.KeptReferenced != 0 {
		t.Errorf("got deleted=%d keptReferenced=%d, want 1/0", got.Deleted, got.KeptReferenced)
	}
	if fileExists(path(ref)) {
		t.Error("look-alike ref kept the file")
	}
}

func TestSweepStagingFollowsRefChangesBetweenCalls(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	now := mtime.Add(2 * time.Hour)
	cfg, path := newSweepFixture(t, mtime, ref)
	refs := &fakeRefLister{refs: map[string]bool{ref: true}}

	got := runSweepFixture(t, cfg, refs, true, now)
	if got.Deleted != 0 || got.KeptReferenced != 1 || !fileExists(path(ref)) {
		t.Fatalf("first call: deleted=%d keptReferenced=%d exists=%v, want 0/1/true", got.Deleted, got.KeptReferenced, fileExists(path(ref)))
	}

	refs.refs = map[string]bool{}
	got = runSweepFixture(t, cfg, refs, true, now)
	if got.Deleted != 1 || got.KeptReferenced != 0 || fileExists(path(ref)) {
		t.Errorf("second call: deleted=%d keptReferenced=%d exists=%v, want 1/0/false", got.Deleted, got.KeptReferenced, fileExists(path(ref)))
	}
}

type refusingDeleteStore struct{ library.AudioStore }

func (refusingDeleteStore) Delete(context.Context, string) error {
	return errors.New("bucket refused the delete")
}

func TestSweepCountsADeleteTheStoreRefusesAsFailedAndKeepsTheObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	cfg, path := newSweepFixture(t, mtime, ref)
	store, err := library.NewAudioStoreFromConfig(cfg, slog.Default())
	if err != nil {
		t.Fatalf("storage.FromConfig: %v", err)
	}
	lister, ok := store.(library.AudioAgeLister)
	if !ok {
		t.Fatal("store is not an AudioAgeLister")
	}

	got, err := stagingsweep.Sweep(context.Background(), refusingDeleteStore{store}, lister, &fakeRefLister{}, mtime.Add(2*time.Hour))
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if want := (stagingsweep.Tally{Failed: 1}); got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
	if !fileExists(path(ref)) {
		t.Error("the object whose delete was refused is gone")
	}
}
