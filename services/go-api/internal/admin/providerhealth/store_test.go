package providerhealth

import "testing"

func TestStore_StatusMixAndCurrent(t *testing.T) {
	s := NewStore()
	s.Record("discogs", "ok", 120)
	s.Record("discogs", "circuit_open", 0)
	s.Record("discogs", "circuit_open", 0)
	s.Record("deezer", "ok", 80)

	snap := s.Snapshot()
	if len(snap) != 2 {
		t.Fatalf("snapshot len = %d, want 2", len(snap))
	}

	if snap[0].Provider != "deezer" || snap[1].Provider != "discogs" {
		t.Fatalf("providers not ordered by name: %s, %s", snap[0].Provider, snap[1].Provider)
	}

	discogs := snap[1]
	if discogs.CurrentStatus != "circuit_open" {
		t.Errorf("current = %q, want circuit_open (most recent)", discogs.CurrentStatus)
	}
	if discogs.CountsPerStatus["circuit_open"] != 2 || discogs.CountsPerStatus["ok"] != 1 {
		t.Errorf("counts = %v, want 2 circuit_open + 1 ok", discogs.CountsPerStatus)
	}
	if discogs.TotalCalls != 3 {
		t.Errorf("total = %d, want 3", discogs.TotalCalls)
	}
}

func TestStore_AvgLatency(t *testing.T) {
	s := NewStore()
	s.Record("deezer", "ok", 100)
	s.Record("deezer", "ok", 200)

	snap := s.Snapshot()
	if snap[0].AvgLatencyMs != 150 {
		t.Errorf("avg latency = %d, want 150", snap[0].AvgLatencyMs)
	}
}
