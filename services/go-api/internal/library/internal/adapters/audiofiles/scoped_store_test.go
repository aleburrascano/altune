package audiofiles_test

import (
	"altune/go-api/internal/library"
	"context"
	"testing"
	"time"
)

type scopedFakeStore struct {
	stored      map[string]bool
	deletedRefs []string
}

func newScopedFakeStore() *scopedFakeStore {
	return &scopedFakeStore{stored: map[string]bool{}}
}

func (f *scopedFakeStore) Exists(_ context.Context, audioRef string) (bool, error) {
	return f.stored[audioRef], nil
}

func (f *scopedFakeStore) Store(_ context.Context, _ string, audioRef string) error {
	f.stored[audioRef] = true
	return nil
}

func (f *scopedFakeStore) Stream(_ context.Context, _ string) (library.AudioStream, int64, error) {
	return nil, 0, nil
}

func (f *scopedFakeStore) Delete(_ context.Context, audioRef string) error {
	f.deletedRefs = append(f.deletedRefs, audioRef)
	delete(f.stored, audioRef)
	return nil
}

type scopedFakeSigner struct{}

func (scopedFakeSigner) SignsLinks() bool { return true }

func (scopedFakeSigner) PresignGet(_ context.Context, audioRef string, _ time.Duration) (string, error) {
	return "signed:" + audioRef, nil
}

type scopedFakeAgeLister struct {
	ages []library.ObjectAge
}

func (f scopedFakeAgeLister) ListWithAge(_ context.Context, _ string) ([]library.ObjectAge, error) {
	return f.ages, nil
}

func scopedBackend(inner *scopedFakeStore, ages []library.ObjectAge) library.AudioBackend {
	return library.NewScopedAudioBackend(library.AudioBackend{
		AudioStore: inner,
		Links:      scopedFakeSigner{},
		Ages:       scopedFakeAgeLister{ages: ages},
	}, "staging/")
}

func TestScopedAudioStore_DeleteOutsidePrefixIsNoop(t *testing.T) {
	inner := newScopedFakeStore()
	inner.stored["prod-user/a/b/c.mp3"] = true
	store := scopedBackend(inner, nil)

	if err := store.Delete(context.Background(), "prod-user/a/b/c.mp3"); err != nil {
		t.Fatalf("expected delete outside prefix to no-op, got error: %v", err)
	}
	if len(inner.deletedRefs) != 0 {
		t.Errorf("expected inner Delete never called, got %v", inner.deletedRefs)
	}
	if !inner.stored["prod-user/a/b/c.mp3"] {
		t.Error("object outside prefix must survive the no-op delete")
	}
}

func TestScopedAudioStore_DeleteInsidePrefixPassesThrough(t *testing.T) {
	inner := newScopedFakeStore()
	inner.stored["staging/u/a/b/c.mp3"] = true
	store := scopedBackend(inner, nil)

	if err := store.Delete(context.Background(), "staging/u/a/b/c.mp3"); err != nil {
		t.Fatalf("expected delete inside prefix to succeed, got error: %v", err)
	}
	if len(inner.deletedRefs) != 1 || inner.deletedRefs[0] != "staging/u/a/b/c.mp3" {
		t.Errorf("expected inner Delete called with the ref, got %v", inner.deletedRefs)
	}
}

func TestScopedAudioStore_StoreOutsidePrefixErrors(t *testing.T) {
	inner := newScopedFakeStore()
	store := scopedBackend(inner, nil)

	err := store.Store(context.Background(), "/tmp/x.mp3", "prod-user/a/b/c.mp3")
	if err == nil {
		t.Fatal("expected an error writing outside the prefix")
	}
	if inner.stored["prod-user/a/b/c.mp3"] {
		t.Error("inner store must never receive a write outside the prefix")
	}
}

func TestScopedAudioStore_StoreInsidePrefixPassesThrough(t *testing.T) {
	inner := newScopedFakeStore()
	store := scopedBackend(inner, nil)

	if err := store.Store(context.Background(), "/tmp/x.mp3", "staging/u/a/b/c.mp3"); err != nil {
		t.Fatalf("expected write inside the prefix to succeed, got error: %v", err)
	}
	if !inner.stored["staging/u/a/b/c.mp3"] {
		t.Error("expected inner store to receive the write")
	}
}

func TestScopedAudioBackend_ForwardsPresignGet(t *testing.T) {
	store := scopedBackend(newScopedFakeStore(), nil)

	if !store.Links.SignsLinks() {
		t.Fatal("the scoped backend must keep the signer it was given")
	}
	url, err := store.Links.PresignGet(context.Background(), "staging/u/a/b/c.mp3", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "signed:staging/u/a/b/c.mp3" {
		t.Errorf("expected the presign call forwarded to the inner signer, got %q", url)
	}
}

func TestScopedAudioBackend_KeepsAnAbsentSignerAbsent(t *testing.T) {
	store := library.NewScopedAudioBackend(library.AudioBackend{
		AudioStore: newScopedFakeStore(),
		Links:      library.NoAudioLinks{},
		Ages:       library.NoAgeListing{},
	}, "staging/")

	if store.Links.SignsLinks() {
		t.Fatal("a backend whose store cannot sign must stay unable to sign once scoped")
	}
}

func TestScopedAudioBackend_ForwardsListWithAge(t *testing.T) {
	store := scopedBackend(newScopedFakeStore(), []library.ObjectAge{{AudioRef: "staging/u/one.mp3", LastModified: time.Unix(0, 0)}})

	objects, err := store.Ages.ListWithAge(context.Background(), "staging/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 || objects[0].AudioRef != "staging/u/one.mp3" {
		t.Errorf("expected the ListWithAge call forwarded to the inner store, got %v", objects)
	}
}

func TestScopedAudioBackend_ListWithAgeOutsidePrefixErrors(t *testing.T) {
	store := scopedBackend(newScopedFakeStore(), nil)

	if _, err := store.Ages.ListWithAge(context.Background(), "prod-user/"); err == nil {
		t.Fatal("expected an error listing outside the prefix")
	}
}
