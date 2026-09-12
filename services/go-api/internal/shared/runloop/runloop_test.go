package runloop

import "testing"

// TestBackground_PauseResumeGate reproduces the runtime kill-switch gap: a
// background loop needs a pause/resume affordance so it can be disabled without
// a restart. The gate must default to running and flip on Pause/Resume.
func TestBackground_PauseResumeGate(t *testing.T) {
	var b Background

	if b.Paused() {
		t.Fatal("a fresh Background must not be paused")
	}

	b.Pause()
	if !b.Paused() {
		t.Fatal("Pause() must engage the gate")
	}

	b.Resume()
	if b.Paused() {
		t.Fatal("Resume() must clear the gate")
	}
}
