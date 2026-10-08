package domain

import "testing"

func TestStringExtra(t *testing.T) {
	cases := []struct {
		name   string
		extras map[string]any
		want   string
	}{
		{"nil map", nil, ""},
		{"missing key", map[string]any{"other": "x"}, ""},
		{"string value", map[string]any{ExtraUPC: "123"}, "123"},
		{"empty string", map[string]any{ExtraUPC: ""}, ""},
		{"non-string value", map[string]any{ExtraUPC: 123}, ""},
		{"nil value", map[string]any{ExtraUPC: nil}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := StringExtra(c.extras, ExtraUPC); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
