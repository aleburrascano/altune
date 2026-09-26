package commands

import (
	"testing"
	"time"
)

func TestOldEnoughToSweep(t *testing.T) {
	cases := []struct {
		name string
		age  time.Duration
		want bool
	}{
		{name: "just uploaded", age: 0, want: false},
		{name: "under the hour", age: 59 * time.Minute, want: false},
		{name: "exactly one hour", age: time.Hour, want: true},
		{name: "well over an hour", age: 3 * time.Hour, want: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oldEnoughToSweep(tc.age); got != tc.want {
				t.Errorf("oldEnoughToSweep(%s) = %v, want %v", tc.age, got, tc.want)
			}
		})
	}
}
