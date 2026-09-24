package service

import (
	"strconv"
	"testing"
	"time"
)

func TestAdmission_ZeroSustainedCapIsSkippedInAdmitAndRefund(t *testing.T) {
	const attempts = 6
	cases := []struct {
		name         string
		sustained    int
		wantAdmitted int
		wantRefunded int
	}{
		{name: "zero disables the sustained cap", sustained: 0, wantAdmitted: attempts, wantRefunded: attempts},
		{name: "negative disables the sustained cap", sustained: -1, wantAdmitted: attempts, wantRefunded: attempts},
		{name: "a cap of one admits one and refunds none", sustained: 1, wantAdmitted: 1, wantRefunded: 0},
		{name: "a cap of four refunds half of it", sustained: 4, wantAdmitted: 4, wantRefunded: 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			limits := SubmissionLimits{
				PerUser:               100,
				PerUserWindow:         time.Hour,
				Global:                100,
				GlobalWindow:          time.Hour,
				GlobalSustained:       tc.sustained,
				GlobalSustainedWindow: time.Hour,
			}

			if got := admittedOf(limits, attempts); got != tc.wantAdmitted {
				t.Fatalf("admitted %d of %d, want %d", got, attempts, tc.wantAdmitted)
			}
			if got := refundedOf(limits, attempts); got != tc.wantRefunded {
				t.Fatalf("refunded %d of %d failed creates, want %d", got, attempts, tc.wantRefunded)
			}
		})
	}
}

func admittedOf(limits SubmissionLimits, attempts int) int {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	a := newSubmissionAdmission(limits, clock.now)
	admitted := 0
	for i := 0; i < attempts; i++ {
		if admitErr(a, "user-"+strconv.Itoa(i)) == nil {
			admitted++
		}
	}
	return admitted
}

func refundedOf(limits SubmissionLimits, attempts int) int {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	a := newSubmissionAdmission(limits, clock.now)
	refunded := 0
	for i := 0; i < attempts; i++ {
		slot, err := a.admit("user-" + strconv.Itoa(i))
		if err == nil && a.refund(slot) {
			refunded++
		}
	}
	return refunded
}
