package core_test

import (
	"altune/overseer/internal/core"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestSLOWindowStaysUnmeasuredBelowItsMinimumCount(t *testing.T) {
	w := core.NewSLOWindow(core.SLO{Journey: "find music", Name: "availability", Target: 0.99, MinTotal: 200})
	w.Record(sloEpoch, 0, 199)

	below := w.Status(sloEpoch)
	w.Record(sloEpoch, 0, 1)
	at := w.Status(sloEpoch)

	if below.Measured || below.Met || below.Total != 199 {
		t.Fatalf("status at 199 of 200 required = %+v, want unmeasured with the tally kept", below)
	}
	if !at.Measured || at.Met {
		t.Fatalf("status at 200 of 200 required = %+v, want measured and missed", at)
	}
}

func TestRegisteredSLOsMatchTheRegisterFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", ".claude", "slos.tsv"))
	if err != nil {
		t.Fatalf("reading the SLO register: %v", err)
	}
	rows := strings.Split(strings.TrimSpace(string(raw)), "\n")
	registered := []core.SLO{core.SearchAvailabilitySLO, core.SearchLatencySLO, core.SongAcquiredSLO}
	if len(rows) != len(registered) {
		t.Fatalf("the register has %d rows and the code registers %d SLOs", len(rows), len(registered))
	}
	for i, row := range rows {
		cols := strings.Split(row, "\t")
		if len(cols) < 6 {
			t.Fatalf("register row %d has %d columns, want at least 6: %q", i+1, len(cols), row)
		}
		target, err := strconv.ParseFloat(strings.TrimSuffix(cols[3], "%"), 64)
		if err != nil {
			t.Fatalf("register row %d target %q: %v", i+1, cols[3], err)
		}
		minimum, err := strconv.ParseUint(cols[5], 10, 64)
		if err != nil {
			t.Fatalf("register row %d minimum count %q: %v", i+1, cols[5], err)
		}
		slo := registered[i]
		if cols[0] != slo.Journey || target/100 != slo.Target || minimum != slo.MinTotal {
			t.Errorf("register row %d is %q at %v%% with minimum %d, code has %q at %v with minimum %d",
				i+1, cols[0], target, minimum, slo.Journey, slo.Target, slo.MinTotal)
		}
	}
}
