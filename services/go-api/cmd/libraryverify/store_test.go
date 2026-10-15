package main

import (
	"errors"
	"strings"
	"testing"
)

func TestParseStatus_NarrowsTheStoredStatusAndRefusesAnythingElse(t *testing.T) {
	for raw, want := range map[string]identityStatus{
		"bound": statusBound, "unofficial": statusUnofficial, "unbound": statusUnbound, "": statusNone,
	} {
		if got, err := parseStatus(raw); err != nil || got != want {
			t.Errorf("parseStatus(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := parseStatus("pending"); !errors.Is(err, errUnknownStatus) {
		t.Errorf("err = %v, want errUnknownStatus", err)
	}
}

func TestReadyTracksSQL_OnlyReadsAndKeepsEveryReadyTrack(t *testing.T) {
	for _, forbidden := range []string{"INSERT", "UPDATE", "DELETE", "ALTER", "DROP", "TRUNCATE"} {
		if strings.Contains(strings.ToUpper(readyTracksSQL), forbidden) {
			t.Errorf("readyTracksSQL contains %s", forbidden)
		}
	}
	if !strings.Contains(readyTracksSQL, "LEFT JOIN track_identity") || !strings.Contains(readyTracksSQL, "acquisition_status = 'ready'") {
		t.Error("readyTracksSQL must keep ready tracks that have no identity row")
	}
}
