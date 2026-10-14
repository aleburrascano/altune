package main

import "testing"

func text(s string) *string { return &s }

func number(f float64) *float64 { return &f }

func readyRow() trackRow {
	return trackRow{ISRC: text("USALBUM"), Version: 3, Status: "ready", DurationSeconds: number(226)}
}

func reacquireChange() change {
	return change{TrackID: "t", UserID: "u", Version: 3, Action: actionReacquire, OldISRC: "USALBUM", FileSeconds: 226, CatalogSeconds: 379}
}

func TestDecide(t *testing.T) {
	cases := []struct {
		name string
		edit func(*trackRow)
		want verdict
	}{
		{"untouched ready track", func(*trackRow) {}, verdictApplied},
		{"version moved", func(r *trackRow) { r.Version = 4 }, verdictStale},
		{"isrc changed since the audit", func(r *trackRow) { r.ISRC = text("USOTHER") }, verdictISRCChanged},
		{"isrc cleared since the audit", func(r *trackRow) { r.ISRC = nil }, verdictISRCChanged},
		{"track no longer ready", func(r *trackRow) { r.Status = "pending" }, verdictNotReady},
		{"job already in flight", func(r *trackRow) { r.InFlight = true }, verdictInFlight},
		{"file length changed since the audit", func(r *trackRow) { r.DurationSeconds = number(300) }, verdictFileChanged},
		{"no file length", func(r *trackRow) { r.DurationSeconds = nil }, verdictFileChanged},
		{"re-acquire already logged", func(r *trackRow) { r.PriorReacquire = true }, verdictAlreadyEnqueued},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := readyRow()
			tc.edit(&row)
			if got := decide(reacquireChange(), row); got != tc.want {
				t.Fatalf("got %s want %s", got, tc.want)
			}
		})
	}
}

func TestDecide_AnISRCFixIgnoresAPriorReacquire(t *testing.T) {
	row := readyRow()
	row.PriorReacquire = true
	fix := reacquireChange()
	fix.Action = actionFixISRC
	if got := decide(fix, row); got != verdictApplied {
		t.Fatalf("got %s", got)
	}
}
