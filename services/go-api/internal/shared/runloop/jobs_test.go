package runloop

import (
	"errors"
	"testing"
)

func TestJobRegistry_EmptyHasNoHealthAndRejectsUnknownName(t *testing.T) {
	var r JobRegistry
	if got := r.Health(); len(got) != 0 {
		t.Fatalf("health rows = %d, want 0", len(got))
	}
	if _, ok := r.SetEnabled("ghost", false); ok {
		t.Fatal("SetEnabled on unknown name returned true")
	}
}

func TestJobControl_RecordTracksConsecutiveFailures(t *testing.T) {
	var r JobRegistry
	jc := r.Control("job")
	jc.Record(errors.New("boom"))
	if got := jc.ConsecutiveFailures(); got != 1 {
		t.Fatalf("after failure = %d, want 1", got)
	}
	jc.Record(nil)
	if got := jc.ConsecutiveFailures(); got != 0 {
		t.Fatalf("after success = %d, want 0", got)
	}
	h := r.Health()[0]
	if h.LastFailure.IsZero() || h.LastSuccess.IsZero() || h.Failures != 1 {
		t.Fatalf("health = %+v", h)
	}
}

func TestJobControl_DisabledAdmitSkips(t *testing.T) {
	var r JobRegistry
	jc := r.Control("job")
	if !jc.Admit() {
		t.Fatal("enabled job refused")
	}
	h, ok := r.SetEnabled("job", false)
	if !ok || h.Enabled {
		t.Fatalf("SetEnabled = %+v, %v", h, ok)
	}
	if jc.Admit() || !jc.Disabled() {
		t.Fatal("disabled job admitted")
	}
	if got := r.Health()[0].Skipped; got != 1 {
		t.Fatalf("skipped = %d, want 1", got)
	}
}

func TestJobControl_DisableIfEnabledFlipsOnce(t *testing.T) {
	var r JobRegistry
	jc := r.Control("job")
	if !jc.DisableIfEnabled() {
		t.Fatal("first call = false")
	}
	if jc.DisableIfEnabled() {
		t.Fatal("second call = true")
	}
}
