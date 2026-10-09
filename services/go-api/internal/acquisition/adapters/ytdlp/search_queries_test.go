package ytdlp

import (
	"altune/go-api/internal/acquisition/ports"
	"testing"
)

func TestSearchQueries(t *testing.T) {
	tests := []struct {
		name        string
		req         ports.FindRequest
		wantQueries []string
		wantMin     int
	}{
		{
			name: "basic track produces title+artist queries",
			req:  ports.FindRequest{Title: "Blinding Lights", Artist: "The Weeknd"},
			wantQueries: []string{
				"Blinding Lights The Weeknd",
				"Blinding Lights The Weeknd audio",
			},
			wantMin: 2,
		},
		{
			name: "track with album adds album query",
			req:  ports.FindRequest{Title: "Circles", Artist: "Post Malone", Album: "Hollywood's Bleeding"},
			wantQueries: []string{
				"Circles Post Malone",
				"Circles Post Malone Hollywood's Bleeding",
				"Circles Post Malone audio",
			},
			wantMin: 3,
		},
		{
			name: "track with ISRC includes ISRC as first query",
			req:  ports.FindRequest{Title: "HUMBLE.", Artist: "Kendrick Lamar", ISRC: "USUM71700626"},
			wantQueries: []string{
				"USUM71700626",
				"HUMBLE. Kendrick Lamar",
			},
			wantMin: 3,
		},
		{
			name:    "empty album skips album query",
			req:     ports.FindRequest{Title: "Song", Artist: "Artist", Album: ""},
			wantMin: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			queries := searchQueries(tt.req)
			if len(queries) < tt.wantMin {
				t.Errorf("expected at least %d queries, got %d: %v", tt.wantMin, len(queries), queries)
			}
			for _, want := range tt.wantQueries {
				found := false
				for _, got := range queries {
					if got == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected query %q in %v", want, queries)
				}
			}
			t.Logf("queries: %v", queries)
		})
	}
}
