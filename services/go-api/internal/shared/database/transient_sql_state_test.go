package database

import "testing"

func TestIsTransientSQLState(t *testing.T) {
	cases := map[string]bool{
		"57P01": true,
		"57P02": true,
		"57P03": true,
		"40001": true,
		"40P01": true,
		"08006": true,
		"08000": true,
		"53300": true,
		"53000": true,
		"23505": false,
		"42P01": false,
		"":      false,
	}
	for code, want := range cases {
		t.Run(code, func(t *testing.T) {
			if got := IsTransientSQLState(code); got != want {
				t.Fatalf("IsTransientSQLState(%q) = %v, want %v", code, got, want)
			}
		})
	}
}
