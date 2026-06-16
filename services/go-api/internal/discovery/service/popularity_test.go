package service

import (
	"testing"
)

func TestNormalizePopularity(t *testing.T) {
	tests := []struct {
		name   string
		extras map[string]any
		minPop int64
		maxPop int64
	}{
		{
			name:   "no metrics returns zero",
			extras: map[string]any{"album": "some album"},
			minPop: 0,
			maxPop: 0,
		},
		{
			name:   "nil extras returns zero",
			extras: nil,
			minPop: 0,
			maxPop: 0,
		},
		{
			name:   "deezer nb_fan high",
			extras: map[string]any{"nb_fan": int64(5_000_000)},
			minPop: 60,
			maxPop: 100,
		},
		{
			name:   "lastfm listeners high as string",
			extras: map[string]any{"listeners": "100000000"},
			minPop: 60,
			maxPop: 100,
		},
		{
			name:   "lastfm listeners as string",
			extras: map[string]any{"listeners": "50000000"},
			minPop: 60,
			maxPop: 100,
		},
		{
			name:   "soundcloud playback_count moderate",
			extras: map[string]any{"playback_count": int64(500_000)},
			minPop: 40,
			maxPop: 80,
		},
		{
			name:   "extreme value capped at 100",
			extras: map[string]any{"listeners": "10000000000000"},
			minPop: 100,
			maxPop: 100,
		},
		{
			name:   "deezer rank 1 most popular",
			extras: map[string]any{"rank": int64(1)},
			minPop: 80,
			maxPop: 100,
		},
		{
			name:   "deezer rank 999999 low popularity",
			extras: map[string]any{"rank": int64(999_999)},
			minPop: 0,
			maxPop: 5,
		},
		{
			name:   "lastfm non-numeric string returns zero for metric",
			extras: map[string]any{"listeners": "not-a-number"},
			minPop: 0,
			maxPop: 0,
		},
		{
			name:   "lastfm string with leading zeros",
			extras: map[string]any{"listeners": "0000100"},
			minPop: 1,
			maxPop: 30,
		},
		{
			name:   "max-of-normalized picks highest",
			extras: map[string]any{"nb_fan": int64(100), "playback_count": int64(500_000)},
			minPop: 40,
			maxPop: 80,
		},
		{
			name:   "zero count returns zero",
			extras: map[string]any{"nb_fan": int64(0)},
			minPop: 0,
			maxPop: 0,
		},
		{
			name:   "negative rank returns zero",
			extras: map[string]any{"rank": int64(-5)},
			minPop: 0,
			maxPop: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizePopularity(tt.extras)
			if got < tt.minPop || got > tt.maxPop {
				t.Errorf("NormalizePopularity() = %d, want [%d, %d]",
					got, tt.minPop, tt.maxPop)
			}
		})
	}
}
