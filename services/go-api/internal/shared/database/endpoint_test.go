package database_test

import (
	"altune/go-api/internal/shared/database"
	"testing"
)

func TestParseEndpoint(t *testing.T) {
	cases := map[string]struct {
		url  string
		want database.Endpoint
	}{
		"sslmode=require":    {"postgres://u:p@db.example.com/x?sslmode=require", database.Endpoint{Host: "db.example.com", RequiresTLS: true}},
		"sslmode=disable":    {"postgres://u:p@db.example.com/x?sslmode=disable", database.Endpoint{Host: "db.example.com"}},
		"sslmode=prefer":     {"postgres://u:p@db.example.com/x?sslmode=prefer", database.Endpoint{Host: "db.example.com", RequiresTLS: true, FallsBackToPlaintext: true}},
		"a loopback host":    {"postgres://u:p@127.0.0.1:5432/x?sslmode=disable", database.Endpoint{Host: "127.0.0.1"}},
		"verify-full":        {"postgres://u:p@db.example.com/x?sslmode=verify-full", database.Endpoint{Host: "db.example.com", RequiresTLS: true}},
		"a key=value string": {"host=db.example.com user=u sslmode=require", database.Endpoint{Host: "db.example.com", RequiresTLS: true}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := database.ParseEndpoint(tc.url)
			if err != nil {
				t.Fatalf("ParseEndpoint: %v", err)
			}
			if got != tc.want {
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
