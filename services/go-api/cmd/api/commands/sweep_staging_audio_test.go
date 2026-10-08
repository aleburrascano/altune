package commands

import (
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared/config"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOldEnoughToSweep(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "just uploaded", age: 0, want: false},
		{name: "under the hour", age: 59 * time.Minute, want: false},
		{name: "exactly one hour", age: time.Hour, want: true},
		{name: "well over an hour", age: 3 * time.Hour, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oldEnoughToSweep(tc.age); got != tc.want {
				t.Errorf("oldEnoughToSweep(%s) = %v, want %v", tc.age, got, tc.want)
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

func runSweepFixture(t *testing.T, cfg *config.Config, refs *fakeRefLister, execute bool, now time.Time) (deleted, keptReferenced, keptFresh, failed int) {
	t.Helper()
	store, err := NewAudioStoreFromConfig(cfg)
	if err != nil {
		t.Fatalf("NewAudioStoreFromConfig: %v", err)
	}
	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		t.Fatal("store is not an AudioAgeLister")
	}
	deleted, keptReferenced, keptFresh, failed, err = sweepStaging(context.Background(), store, lister, refs, execute, now)
	if err != nil {
		t.Fatalf("sweepStaging: %v", err)
	}
	return
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSweepStagingNoObjects(t *testing.T) {
	cfg, _ := newSweepFixture(t, time.Now())
	d, r, f, x := runSweepFixture(t, cfg, &fakeRefLister{}, true, time.Now())
	if d != 0 || r != 0 || f != 0 || x != 0 {
		t.Errorf("got %d/%d/%d/%d, want all zeros", d, r, f, x)
	}
}

func TestSweepStagingKeepsReferencedOldObject(t *testing.T) {
	const ref = "staging/a.mp3"
	mtime := time.Now()
	cfg, path := newSweepFixture(t, mtime, ref)
	d, r, f, x := runSweepFixture(t, cfg, &fakeRefLister{refs: map[string]bool{ref: true}}, true, mtime.Add(2*time.Hour))
	if d != 0 || r != 1 || f != 0 || x != 0 {
		t.Errorf("got %d/%d/%d/%d, want 0/1/0/0", d, r, f, x)
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
		d, r, f, x := runSweepFixture(t, cfg, &fakeRefLister{}, true, now)
		if d != 1 || r != 0 || f != 0 || x != 0 {
			t.Errorf("got %d/%d/%d/%d, want 1/0/0/0", d, r, f, x)
		}
		if fileExists(path(ref)) {
			t.Error("file survived an execute sweep")
		}
	})
	t.Run("dry run keeps the file", func(t *testing.T) {
		cfg, path := newSweepFixture(t, mtime, ref)
		d, r, f, x := runSweepFixture(t, cfg, &fakeRefLister{}, false, now)
		if d != 1 || r != 0 || f != 0 || x != 0 {
			t.Errorf("got %d/%d/%d/%d, want 1/0/0/0", d, r, f, x)
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
	d, r, f, x := runSweepFixture(t, cfg, &fakeRefLister{}, true, mtime.Add(59*time.Minute))
	if d != 0 || r != 0 || f != 1 || x != 0 {
		t.Errorf("got %d/%d/%d/%d, want 0/0/1/0", d, r, f, x)
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
	d, r, _, _ := runSweepFixture(t, cfg, refs, true, mtime.Add(2*time.Hour))
	if d != 1 || r != 0 {
		t.Errorf("got deleted=%d keptReferenced=%d, want 1/0", d, r)
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

	d, r, _, _ := runSweepFixture(t, cfg, refs, true, now)
	if d != 0 || r != 1 || !fileExists(path(ref)) {
		t.Fatalf("first call: deleted=%d keptReferenced=%d exists=%v, want 0/1/true", d, r, fileExists(path(ref)))
	}

	refs.refs = map[string]bool{}
	d, r, _, _ = runSweepFixture(t, cfg, refs, true, now)
	if d != 1 || r != 0 || fileExists(path(ref)) {
		t.Errorf("second call: deleted=%d keptReferenced=%d exists=%v, want 1/0/false", d, r, fileExists(path(ref)))
	}
}
