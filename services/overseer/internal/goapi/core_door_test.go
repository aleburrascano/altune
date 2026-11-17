package goapi_test

import (
	"altune/overseer/internal/goapi"
	"testing"
	"time"
)

func TestPanelReasonNeverPairsLiveWithAFailure(t *testing.T) {
	cases := []struct {
		status        goapi.Status
		failureReason string
		want          string
	}{
		{goapi.StatusUp, goapi.ReasonDown, ""},
		{goapi.StatusConnecting, goapi.ReasonDown, "connecting"},
		{goapi.StatusConnecting, "", "connecting"},
		{goapi.StatusDown, goapi.ReasonAuth, goapi.ReasonAuth},
		{goapi.StatusDown, goapi.ReasonDown, goapi.ReasonDown},
	}
	for _, tc := range cases {
		if got := tc.status.PanelReason(tc.failureReason); got != tc.want {
			t.Errorf("%v.PanelReason(%q) = %q, want %q", tc.status, tc.failureReason, got, tc.want)
		}
	}
}

func TestStatusNamesItsStateAndPanel(t *testing.T) {
	cases := []struct {
		status goapi.Status
		name   string
		panel  string
	}{
		{goapi.StatusUp, "up", "live"},
		{goapi.StatusDown, "down", "source_down"},
		{goapi.StatusConnecting, "connecting", "stale"},
		{goapi.Status(99), "unknown", "stale"},
	}
	for _, tc := range cases {
		if got := tc.status.String(); got != tc.name {
			t.Errorf("Status(%d).String() = %q, want %q", int32(tc.status), got, tc.name)
		}
		if got := tc.status.PanelState(); got != tc.panel {
			t.Errorf("Status(%d).PanelState() = %q, want %q", int32(tc.status), got, tc.panel)
		}
	}
}

func TestCredentialHealthIsOKOnlyAfterARefreshWithNoFailureSince(t *testing.T) {
	refreshed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		health goapi.CredentialHealth
		want   bool
	}{
		{"never refreshed", goapi.CredentialHealth{}, false},
		{"never refreshed, failing", goapi.CredentialHealth{ConsecutiveFailures: 1}, false},
		{"refreshed", goapi.CredentialHealth{LastRefresh: refreshed}, true},
		{"refreshed, failing since", goapi.CredentialHealth{LastRefresh: refreshed, ConsecutiveFailures: 1}, false},
	}
	for _, tc := range cases {
		if got := tc.health.OK(); got != tc.want {
			t.Errorf("%s: OK() = %v, want %v", tc.name, got, tc.want)
		}
	}
}
