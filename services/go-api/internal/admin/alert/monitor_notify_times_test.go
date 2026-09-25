package alert

import (
	"context"
	"errors"
	"testing"
)

func TestMonitorStatus_NotifyTimesFollowOutcome(t *testing.T) {
	firing := true
	m := newTestMonitor(&flakyNotifier{failCalls: 1}, signalCond("k", &firing))

	m.tick(context.Background())
	st := m.Status()
	if st.LastNotifyErrorAt.IsZero() || !st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after failure status = %+v, want only error time", st)
	}

	m.tick(context.Background())
	if st := m.Status(); st.LastNotifyOKAt.IsZero() {
		t.Fatalf("after success status = %+v, want ok time", st)
	}
}

func TestMonitorStatus_NopNotifierNeverRecordsSuccess(t *testing.T) {
	firing := true
	m := newTestMonitor(NopNotifier{}, signalCond("k", &firing))
	m.tick(context.Background())

	if st := m.Status(); !st.LastNotifyOKAt.IsZero() || !st.NopNotifier {
		t.Fatalf("status = %+v, nop must not claim a delivered page", st)
	}
}

func TestMonitorStatus_AlwaysFailingNeverRecordsSuccess(t *testing.T) {
	firing := true
	m := newTestMonitor(&stubNotifier{err: errors.New("dead")}, signalCond("k", &firing))
	m.tick(context.Background())
	if !m.Status().LastNotifyOKAt.IsZero() {
		t.Fatal("ok time set despite failure")
	}
}
