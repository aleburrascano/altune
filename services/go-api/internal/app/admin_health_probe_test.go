package app

import (
	adminHandler "altune/go-api/internal/admin/handler"
	"context"
	"testing"
)

// TestDepStatus_MatchesAdminWireValues pins the app-owned DepStatus values to
// the admin handler's, since adminHealthProbe maps one to the other by
// conversion and the handler's values are the /admin/health wire contract.
func TestDepStatus_MatchesAdminWireValues(t *testing.T) {
	pairs := []struct {
		app   DepStatus
		admin adminHandler.DepStatus
	}{
		{DepUp, adminHandler.DepUp},
		{DepNotConfigured, adminHandler.DepNotConfigured},
		{DepDown, adminHandler.DepDown},
	}
	for _, p := range pairs {
		if string(p.app) != string(p.admin) {
			t.Errorf("app %q != admin %q", p.app, p.admin)
		}
	}
}

func TestAdminHealthProbe_MapsStatuses(t *testing.T) {
	a := &App{}

	got := a.adminHealthProbe(context.Background())

	if got.DB != adminHandler.DepNotConfigured || got.Redis != adminHandler.DepNotConfigured || got.Auth != adminHandler.DepNotConfigured {
		t.Errorf("statuses = %q/%q/%q, want all not_configured", got.DB, got.Redis, got.Auth)
	}
	if !got.Healthy() {
		t.Error("Healthy() = false for unconfigured dependencies, want true")
	}
}
