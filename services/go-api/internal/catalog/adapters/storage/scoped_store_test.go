package storage

import (
	"altune/go-api/internal/catalog/ports"
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

func (f *scopedFakeStore) Stream(_ context.Context, _ string) (ports.AudioStream, int64, error) {
	return nil, 0, nil
}

func (f *scopedFakeStore) Delete(_ context.Context, audioRef string) error {
	f.deletedRefs = append(f.deletedRefs, audioRef)
	delete(f.stored, audioRef)
	return nil
}

type scopedFakeSigningStore struct {
	*scopedFakeStore
}

func (f *scopedFakeSigningStore) PresignGet(_ context.Context, audioRef string, _ time.Duration) (string, error) {
	return "signed:" + audioRef, nil
}

type scopedFakeListingStore struct {
	*scopedFakeStore
}

func (f *scopedFakeListingStore) List(_ context.Context, prefix string) ([]string, error) {
	return []string{prefix + "one.mp3"}, nil
}

type scopedFakeAgeListingStore struct {
	*scopedFakeStore
	ages []ports.ObjectAge
}

func (f *scopedFakeAgeListingStore) ListWithAge(_ context.Context, _ string) ([]ports.ObjectAge, error) {
	return f.ages, nil
}

type scopedFakeCopyingStore struct {
	*scopedFakeStore
	copiedFrom []string
	copiedTo   []string
}

func (f *scopedFakeCopyingStore) Copy(_ context.Context, srcRef, dstRef string) error {
	f.copiedFrom = append(f.copiedFrom, srcRef)
	f.copiedTo = append(f.copiedTo, dstRef)
	f.stored[dstRef] = true
	return nil
}

func TestScopedAudioStore_DeleteOutsidePrefixIsNoop(t *testing.T) {
	inner := newScopedFakeStore()
	inner.stored["prod-user/a/b/c.mp3"] = true
	store := NewScopedAudioStore(inner, "staging/")

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
	store := NewScopedAudioStore(inner, "staging/")

	if err := store.Delete(context.Background(), "staging/u/a/b/c.mp3"); err != nil {
		t.Fatalf("expected delete inside prefix to succeed, got error: %v", err)
	}
	if len(inner.deletedRefs) != 1 || inner.deletedRefs[0] != "staging/u/a/b/c.mp3" {
		t.Errorf("expected inner Delete called with the ref, got %v", inner.deletedRefs)
	}
}

func TestScopedAudioStore_StoreOutsidePrefixErrors(t *testing.T) {
	inner := newScopedFakeStore()
	store := NewScopedAudioStore(inner, "staging/")

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
	store := NewScopedAudioStore(inner, "staging/")

	if err := store.Store(context.Background(), "/tmp/x.mp3", "staging/u/a/b/c.mp3"); err != nil {
		t.Fatalf("expected write inside the prefix to succeed, got error: %v", err)
	}
	if !inner.stored["staging/u/a/b/c.mp3"] {
		t.Error("expected inner store to receive the write")
	}
}

func TestScopedAudioStore_ForwardsPresignGet(t *testing.T) {
	inner := &scopedFakeSigningStore{scopedFakeStore: newScopedFakeStore()}
	store := NewScopedAudioStore(inner, "staging/")

	signer, ok := store.(ports.AudioURLSigner)
	if !ok {
		t.Fatal("expected the scoped store to still satisfy ports.AudioURLSigner")
	}
	url, err := signer.PresignGet(context.Background(), "staging/u/a/b/c.mp3", time.Minute)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if url != "signed:staging/u/a/b/c.mp3" {
		t.Errorf("expected the presign call forwarded to the inner store, got %q", url)
	}
}

func TestScopedAudioStore_NoSignerWhenInnerDoesNotSign(t *testing.T) {
	store := NewScopedAudioStore(newScopedFakeStore(), "staging/")
	if _, ok := store.(ports.AudioURLSigner); ok {
		t.Fatal("expected no AudioURLSigner when the wrapped store does not sign")
	}
}

func TestScopedAudioStore_ForwardsList(t *testing.T) {
	inner := &scopedFakeListingStore{scopedFakeStore: newScopedFakeStore()}
	store := NewScopedAudioStore(inner, "staging/")

	lister, ok := store.(ports.AudioLister)
	if !ok {
		t.Fatal("expected the scoped store to still satisfy ports.AudioLister")
	}
	refs, err := lister.List(context.Background(), "staging/u/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(refs) != 1 || refs[0] != "staging/u/one.mp3" {
		t.Errorf("expected the list call forwarded to the inner store, got %v", refs)
	}
}

func TestScopedAudioStore_NoAgeListerWhenInnerDoesNotListAge(t *testing.T) {
	store := NewScopedAudioStore(newScopedFakeStore(), "staging/")
	if _, ok := store.(ports.AudioAgeLister); ok {
		t.Fatal("expected no AudioAgeLister when the wrapped store does not list with age")
	}
}

func TestScopedAudioStore_ForwardsListWithAge(t *testing.T) {
	inner := &scopedFakeAgeListingStore{
		scopedFakeStore: newScopedFakeStore(),
		ages:            []ports.ObjectAge{{AudioRef: "staging/u/one.mp3", LastModified: time.Unix(0, 0)}},
	}
	store := NewScopedAudioStore(inner, "staging/")

	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		t.Fatal("expected sweep-staging-audio's store.(ports.AudioAgeLister) assertion to still find a lister through the decorator (#3092)")
	}
	objects, err := lister.ListWithAge(context.Background(), "staging/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(objects) != 1 || objects[0].AudioRef != "staging/u/one.mp3" {
		t.Errorf("expected the ListWithAge call forwarded to the inner store, got %v", objects)
	}
}

func TestScopedAudioStore_ListWithAgeOutsidePrefixErrors(t *testing.T) {
	inner := &scopedFakeAgeListingStore{scopedFakeStore: newScopedFakeStore()}
	store := NewScopedAudioStore(inner, "staging/")

	lister, ok := store.(ports.AudioAgeLister)
	if !ok {
		t.Fatal("expected the scoped store to satisfy ports.AudioAgeLister")
	}
	if _, err := lister.ListWithAge(context.Background(), "prod-user/"); err == nil {
		t.Fatal("expected an error listing outside the prefix")
	}
}

func TestScopedAudioStore_NoCopierWhenInnerDoesNotCopy(t *testing.T) {
	store := NewScopedAudioStore(newScopedFakeStore(), "staging/")
	if _, ok := store.(ports.AudioCopier); ok {
		t.Fatal("expected no AudioCopier when the wrapped store does not copy")
	}
}

func TestScopedAudioStore_ForwardsCopyInsidePrefix(t *testing.T) {
	inner := &scopedFakeCopyingStore{scopedFakeStore: newScopedFakeStore()}
	store := NewScopedAudioStore(inner, "staging/")

	copier, ok := store.(ports.AudioCopier)
	if !ok {
		t.Fatal("expected the scoped store to satisfy ports.AudioCopier")
	}
	if err := copier.Copy(context.Background(), "staging/u/a.mp3", "staging/u/b.mp3"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(inner.copiedFrom) != 1 || inner.copiedFrom[0] != "staging/u/a.mp3" || inner.copiedTo[0] != "staging/u/b.mp3" {
		t.Errorf("expected the copy call forwarded to the inner store, got from=%v to=%v", inner.copiedFrom, inner.copiedTo)
	}
}

func TestScopedAudioStore_CopyOutsidePrefixErrors(t *testing.T) {
	inner := &scopedFakeCopyingStore{scopedFakeStore: newScopedFakeStore()}
	store := NewScopedAudioStore(inner, "staging/")

	copier, ok := store.(ports.AudioCopier)
	if !ok {
		t.Fatal("expected the scoped store to satisfy ports.AudioCopier")
	}
	if err := copier.Copy(context.Background(), "staging/u/a.mp3", "prod-user/b.mp3"); err == nil {
		t.Fatal("expected an error copying out to a prod key through the scoped store")
	}
	if len(inner.copiedFrom) != 0 {
		t.Error("expected the inner Copy never called for a destination outside the prefix")
	}
}
