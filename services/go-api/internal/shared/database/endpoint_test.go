package database_test

import (
	"altune/go-api/internal/shared/database"
	"testing"
)

type wantEndpoint struct {
	Host                 string
	RequiresTLS          bool
	FallsBackToPlaintext bool
}

func TestParseEndpoint(t *testing.T) {
	cases := map[string]struct {
		url  string
		want wantEndpoint
	}{
		"sslmode=require":    {"postgres://u:p@db.example.com/x?sslmode=require", wantEndpoint{Host: "db.example.com", RequiresTLS: true}},
		"sslmode=disable":    {"postgres://u:p@db.example.com/x?sslmode=disable", wantEndpoint{Host: "db.example.com"}},
		"sslmode=prefer":     {"postgres://u:p@db.example.com/x?sslmode=prefer", wantEndpoint{Host: "db.example.com", RequiresTLS: true, FallsBackToPlaintext: true}},
		"a loopback host":    {"postgres://u:p@127.0.0.1:5432/x?sslmode=disable", wantEndpoint{Host: "127.0.0.1"}},
		"sslmode=allow":      {"postgres://u:p@db.example.com/x?sslmode=allow", wantEndpoint{Host: "db.example.com"}},
		"no sslmode":         {"postgres://u:p@db.example.com/x", wantEndpoint{Host: "db.example.com", RequiresTLS: true, FallsBackToPlaintext: true}},
		"verify-ca":          {"postgres://u:p@db.example.com/x?sslmode=verify-ca", wantEndpoint{Host: "db.example.com", RequiresTLS: true}},
		"verify-full":        {"postgres://u:p@db.example.com/x?sslmode=verify-full", wantEndpoint{Host: "db.example.com", RequiresTLS: true}},
		"a key=value string": {"host=db.example.com user=u sslmode=require", wantEndpoint{Host: "db.example.com", RequiresTLS: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := database.ParseEndpoint(tc.url)
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if got.Host != tc.want.Host || got.RequiresTLS != tc.want.RequiresTLS || got.FallsBackToPlaintext != tc.want.FallsBackToPlaintext {
				t.Fatalf("ParseEndpoint = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestParseEndpointRefusesAMalformedURL(t *testing.T) {
	if _, err := database.ParseEndpoint("postgres://u:p@host:notaport/x"); err == nil {
		t.Fatal("a malformed URL must be an error")
	}
}
