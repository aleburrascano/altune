package audio_test

import (
	"altune/go-api/internal/library"
	"altune/go-api/internal/library/librarytest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"
)

const audioURLLifetime = time.Hour

type recordingSigner struct {
	*library.InMemoryAudioStore
	ttls []time.Duration
	err  error
}

func newRecordingSigner() *recordingSigner {
	return &recordingSigner{InMemoryAudioStore: library.NewInMemoryAudioStore()}
}

func (s *recordingSigner) PresignGet(_ context.Context, audioRef string, ttl time.Duration) (string, error) {
	s.ttls = append(s.ttls, ttl)
	if s.err != nil {
		return "", s.err
	}
	return "https://signed.example/" + audioRef, nil
}

type failingTrackLister struct {
	*library.InMemoryTrackRepo
	err error
}

func (r failingTrackLister) ListByIDs(context.Context, ids.UserId, []library.TrackId) ([]*library.Track, error) {
	return nil, r.err
}

func TestAudioURLService_Resolve(t *testing.T) {
	ctx := context.Background()
	userId := librarytest.UserId()

	t.Run("ready tracks get signed urls, non-streamable and unknown ids are skipped", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		pending := librarytest.SeedTrack(t, repo, userId, "Pending", "Artist", "Album")
		svc := library.MustNewAudioURLService(repo, newRecordingSigner(), time.Now, library.NoopAudioStoreMetrics())

		out, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID, pending.ID, library.NewTrackId()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("expected 1 url (pending and unknown skipped), got %d", len(out))
		}
		if out[0].TrackID != ready.ID {
			t.Errorf("url track = %v, want %v", out[0].TrackID, ready.ID)
		}
		if out[0].URL != "https://signed.example/audio/ok.opus" {
			t.Errorf("url = %q, unexpected", out[0].URL)
		}
		if !out[0].ExpiresAt.After(time.Now()) {
			t.Error("expires_at should be in the future")
		}
		if out[0].Version != ready.AudioVersion {
			t.Errorf("version = %q, want %q", out[0].Version, ready.AudioVersion)
		}
		if out[0].Version == "" {
			t.Error("a ready track must carry a non-empty audio version")
		}
	})

	t.Run("a re-acquired track resolves under a new version so a cached client copy stops matching", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		svc := library.MustNewAudioURLService(repo, newRecordingSigner(), time.Now, library.NoopAudioStoreMetrics())

		before, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if err := ready.MarkReady("audio/ok.opus"); err != nil {
			t.Fatalf("re-acquire: %v", err)
		}
		if err := repo.Update(ctx, ready, ready.Version); err != nil {
			t.Fatalf("update: %v", err)
		}

		after, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if after[0].URL != before[0].URL {
			t.Fatalf("this case is only meaningful while the object key is stable: %q vs %q", before[0].URL, after[0].URL)
		}
		if after[0].Version == before[0].Version {
			t.Errorf("version stayed %q across a re-acquisition — a client keyed on it would keep serving the old audio", after[0].Version)
		}
	})

	t.Run("a track acquired before the column existed resolves with an empty version", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		ready.AudioVersion = ""
		if err := repo.Update(ctx, ready, ready.Version); err != nil {
			t.Fatalf("update: %v", err)
		}
		svc := library.MustNewAudioURLService(repo, newRecordingSigner(), time.Now, library.NoopAudioStoreMetrics())

		out, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 1 {
			t.Fatalf("expected the track to still resolve, got %d urls", len(out))
		}
		if out[0].Version != "" {
			t.Errorf("version = %q, want empty so the client keeps its existing copy", out[0].Version)
		}
	})

	t.Run("a failed track lookup surfaces the error instead of an empty result", func(t *testing.T) {
		errLookup := errors.New("track lookup down")
		repo := failingTrackLister{InMemoryTrackRepo: library.NewInMemoryTrackRepo(), err: errLookup}
		svc := library.MustNewAudioURLService(repo, newRecordingSigner(), time.Now, library.NoopAudioStoreMetrics())

		out, err := svc.Resolve(ctx, userId, []library.TrackId{library.NewTrackId()})

		if !errors.Is(err, errLookup) {
			t.Fatalf("error = %v, want %v", err, errLookup)
		}
		if out != nil {
			t.Fatalf("resolved = %+v, want nil alongside the error", out)
		}
	})

	t.Run("no signer returns nothing (client proxies)", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		svc := library.MustNewAudioURLService(repo, library.NewInMemoryAudioStore(), time.Now, library.NoopAudioStoreMetrics())

		out, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected no urls without a signer, got %d", len(out))
		}
	})

	t.Run("presign failure skips only that track and increments the failure metric", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		metrics := &library.InMemoryAudioStoreMetrics{}
		signer := newRecordingSigner()
		signer.err = errors.New("boom")
		svc := library.MustNewAudioURLService(repo, signer, time.Now, metrics)

		out, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(out) != 0 {
			t.Fatalf("expected the failed track skipped, got %d", len(out))
		}
		if metrics.PresignFailures != 1 {
			t.Errorf("presign failure metric = %d, want 1 so operators can dashboard the failure rate", metrics.PresignFailures)
		}
	})

	t.Run("each url is signed for one hour and expires one hour past the injected clock", func(t *testing.T) {
		repo := library.NewInMemoryTrackRepo()
		ready := librarytest.SeedReadyTrack(t, repo, userId, "Track", "Artist", "Album", "audio/ok.opus")
		pinned := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
		signer := newRecordingSigner()
		svc := library.MustNewAudioURLService(repo, signer, func() time.Time { return pinned }, library.NoopAudioStoreMetrics())

		out, err := svc.Resolve(ctx, userId, []library.TrackId{ready.ID})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(signer.ttls) != 1 || signer.ttls[0] != audioURLLifetime {
			t.Fatalf("signer ttls = %v, want [%s]", signer.ttls, audioURLLifetime)
		}
		if len(out) != 1 || !out[0].ExpiresAt.Equal(pinned.Add(audioURLLifetime)) {
			t.Errorf("resolved = %+v, want one url expiring exactly at %v", out, pinned.Add(audioURLLifetime))
		}
	})
}

func TestNewAudioURLService_RefusesANilClock(t *testing.T) {
	svc, err := library.NewAudioURLService(library.NewInMemoryTrackRepo(), newRecordingSigner(), nil, library.NoopAudioStoreMetrics())

	if !errors.Is(err, library.ErrAudioClockRequired) || svc != nil {
		t.Fatalf("NewAudioURLService(nil clock) = (%v, %v), want ErrAudioClockRequired and no service", svc, err)
	}
}

func TestNewAudioURLService_RefusesNilMetrics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("construction succeeded, want a panic at construction")
		}
	}()

	_, _ = library.NewAudioURLService(library.NewInMemoryTrackRepo(), newRecordingSigner(), time.Now, nil)
}
