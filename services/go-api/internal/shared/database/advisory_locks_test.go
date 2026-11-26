package database_test

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/dbtest"
	"context"
	"strings"
	"testing"
)

func TestAdvisoryLocks_SessionLocksPingsUnlocksAndNamesWhatFailsOnceReleased(t *testing.T) {
	dbtest.RequireIntegration(t)
	ctx := context.Background()
	session, err := database.NewAdvisoryLocks(dbtest.Pool(t)).Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire = %v", err)
	}
	const key = int64(7_410_001)

	won, err := session.TryAdvisoryLock(ctx, key)
	if err != nil || !won {
		t.Fatalf("TryAdvisoryLock = %v, %v, want true, nil", won, err)
	}
	if err := session.Ping(ctx); err != nil {
		t.Fatalf("Ping = %v, want nil", err)
	}
	if err := session.AdvisoryUnlock(ctx, key); err != nil {
		t.Fatalf("AdvisoryUnlock = %v, want nil", err)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if err := session.Ping(cancelled); err == nil || !strings.Contains(err.Error(), "ping the lock connection") {
		t.Errorf("Ping on a cancelled context = %v, want an error naming the ping", err)
	}
	if err := session.AdvisoryUnlock(cancelled, key); err == nil || !strings.Contains(err.Error(), "advisory unlock") {
		t.Errorf("AdvisoryUnlock on a cancelled context = %v, want an error naming the unlock", err)
	}
	if _, err := session.TryAdvisoryLock(cancelled, key); err == nil || !strings.Contains(err.Error(), "try advisory lock") {
		t.Errorf("TryAdvisoryLock on a cancelled context = %v, want an error naming the lock", err)
	}
	session.Release()
}
