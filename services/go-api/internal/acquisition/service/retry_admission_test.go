package service

import (
	"errors"
	"testing"
	"time"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func failedTrack(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkFailed("yt-dlp exited 1"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	return track
}

func readyTrack(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkReady("audio-ref"); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	return track
}

func pendingTrackOnly(t *testing.T) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	return track
}

func TestCooldownGate_AdmitsFirstCall(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	if !g.admit("k") {
		t.Fatal("admit(k) = false on first call, want true")
	}
	if _, ok := g.lastAt["k"]; !ok {
		t.Error("first admit did not record the key")
	}
}

func TestCooldownGate_DeniesSecondCallWithinCooldown(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	g.admit("k")
	if g.admit("k") {
		t.Error("second admit within cooldown = true, want false")
	}
}

func TestCooldownGate_AdmitsAfterCooldownElapsed(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	g.lastAt["k"] = time.Now().Add(-RetryCooldown - time.Second)
	if !g.admit("k") {
		t.Error("admit after cooldown elapsed = false, want true")
	}
}

func TestCooldownGate_IndependentKeys(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	if !g.admit("a") {
		t.Error("admit(a) = false, want true")
	}
	if !g.admit("b") {
		t.Error("admit(b) = false, want true")
	}
}

func TestCooldownGate_PrunesStaleEntriesOnAdmit(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	g.lastAt["stale"] = time.Now().Add(-2*RetryCooldown - time.Second)
	g.lastAt["recent"] = time.Now().Add(-RetryCooldown - time.Second)

	g.admit("fresh")

	if _, ok := g.lastAt["stale"]; ok {
		t.Error("stale entry (>= 2*cooldown) was not pruned")
	}
	if _, ok := g.lastAt["recent"]; !ok {
		t.Error("recent entry (< 2*cooldown) was pruned")
	}
	if _, ok := g.lastAt["fresh"]; !ok {
		t.Error("admitted key was not recorded")
	}
}

func TestCooldownGate_DeniedAdmitSkipsPrune(t *testing.T) {
	g := newCooldownGate(RetryCooldown)
	g.admit("k")
	g.lastAt["stale"] = time.Now().Add(-2*RetryCooldown - time.Second)

	if g.admit("k") {
		t.Fatal("second admit within cooldown = true, want false")
	}
	if _, ok := g.lastAt["stale"]; !ok {
		t.Error("denied admit pruned entries; the early return should skip the prune loop")
	}
}

func TestRetryAdmission_Admit(t *testing.T) {
	t.Run("not failed yields ErrRetryNotFailed", func(t *testing.T) {
		a := NewRetryAdmission()
		if err := a.Admit(pendingTrackOnly(t)); !errors.Is(err, ErrRetryNotFailed) {
			t.Errorf("Admit = %v, want ErrRetryNotFailed", err)
		}
	})

	t.Run("ready is not failed", func(t *testing.T) {
		a := NewRetryAdmission()
		if err := a.Admit(readyTrack(t)); !errors.Is(err, ErrRetryNotFailed) {
			t.Errorf("Admit = %v, want ErrRetryNotFailed", err)
		}
	})

	t.Run("first failed retry admitted", func(t *testing.T) {
		a := NewRetryAdmission()
		if err := a.Admit(failedTrack(t)); err != nil {
			t.Errorf("Admit = %v, want nil", err)
		}
	})

	t.Run("second failed retry within cooldown yields ErrRetryCooldown", func(t *testing.T) {
		a := NewRetryAdmission()
		track := failedTrack(t)
		if err := a.Admit(track); err != nil {
			t.Fatalf("first Admit = %v, want nil", err)
		}
		if err := a.Admit(track); !errors.Is(err, ErrRetryCooldown) {
			t.Errorf("second Admit = %v, want ErrRetryCooldown", err)
		}
	})

	t.Run("distinct tracks admitted independently", func(t *testing.T) {
		a := NewRetryAdmission()
		if err := a.Admit(failedTrack(t)); err != nil {
			t.Errorf("first track Admit = %v, want nil", err)
		}
		if err := a.Admit(failedTrack(t)); err != nil {
			t.Errorf("second track Admit = %v, want nil", err)
		}
	})
}

func TestReacquireAdmission_Admit(t *testing.T) {
	t.Run("not streamable yields ErrReacquireNotReady", func(t *testing.T) {
		a := NewReacquireAdmission()
		if err := a.Admit(pendingTrackOnly(t)); !errors.Is(err, ErrReacquireNotReady) {
			t.Errorf("Admit = %v, want ErrReacquireNotReady", err)
		}
	})

	t.Run("failed track is not streamable", func(t *testing.T) {
		a := NewReacquireAdmission()
		if err := a.Admit(failedTrack(t)); !errors.Is(err, ErrReacquireNotReady) {
			t.Errorf("Admit = %v, want ErrReacquireNotReady", err)
		}
	})

	t.Run("first streamable reacquire admitted", func(t *testing.T) {
		a := NewReacquireAdmission()
		if err := a.Admit(readyTrack(t)); err != nil {
			t.Errorf("Admit = %v, want nil", err)
		}
	})

	t.Run("second reacquire within cooldown yields ErrRetryCooldown", func(t *testing.T) {
		a := NewReacquireAdmission()
		track := readyTrack(t)
		if err := a.Admit(track); err != nil {
			t.Fatalf("first Admit = %v, want nil", err)
		}
		if err := a.Admit(track); !errors.Is(err, ErrRetryCooldown) {
			t.Errorf("second Admit = %v, want ErrRetryCooldown", err)
		}
	})
}
