package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	"errors"
	"strings"
	"testing"
)

func TestDecodeCapped(t *testing.T) {
	type doc struct {
		A string `json:"a"`
	}
	cases := []struct {
		name    string
		body    string
		limit   int64
		want    string
		wantErr error
	}{
		{"fits", `{"a":"x"}`, 64, "x", nil},
		{"exactly at the limit", `{"a":"x"}`, 9, "x", nil},
		{"past the limit", `{"a":"` + strings.Repeat("y", 100) + `"}`, 16, "", goapi.ErrBodyTooLarge},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got doc
			err := goapi.DecodeCapped(strings.NewReader(tc.body), tc.limit, &got)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("error = %v, want %v", err, tc.wantErr)
			}
			if got.A != tc.want {
				t.Errorf("decoded %q, want %q", got.A, tc.want)
			}
		})
	}
}

func TestDecodeCapped_MalformedJSONIsNotReportedAsTooLarge(t *testing.T) {
	var out map[string]any

	err := goapi.DecodeCapped(strings.NewReader(`{"a":`), 64, &out)

	if err == nil || errors.Is(err, goapi.ErrBodyTooLarge) {
		t.Fatalf("error = %v, want a decode error", err)
	}
}
