package providerrequests_test

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/adapters/providers/providerrequests"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"errors"
	"testing"
)

func TestArtworkFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantMiss bool
		wantIs   []error
	}{
		{name: "404 is a miss", err: outbound.StatusError{Status: 404}, wantMiss: true},
		{name: "400 is a miss", err: outbound.StatusError{Status: 400}, wantMiss: true},
		{name: "410 is refused", err: outbound.StatusError{Status: 410}, wantIs: []error{ports.ErrArtworkUnavailable}},
		{name: "context canceled", err: context.Canceled, wantIs: []error{ports.ErrArtworkUnavailable, context.Canceled}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := providerrequests.ArtworkFailure(domain.ProviderKeyCoverArtArchive, tt.err)
			if got != "" {
				t.Errorf("url = %q, want empty", got)
			}
			if tt.wantMiss {
				if err != nil {
					t.Fatalf("err = %v, want nil", err)
				}
				return
			}
			for _, want := range tt.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("errors.Is(%v, %v) = false, want true", err, want)
				}
			}
		})
	}
}
