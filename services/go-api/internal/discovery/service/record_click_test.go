package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

func TestRecordClickService_Execute(t *testing.T) {
	userID := shared.NewUserId(uuid.New())

	tests := []struct {
		name      string
		repo      *fakeSearchClickRepository
		input     RecordClickInput
		wantErr   bool
		wantNil   bool // expect nil repo path (no-op)
	}{
		{
			name: "click recorded successfully",
			repo: &fakeSearchClickRepository{
				insertIfOutsideWindowFn: func(_ context.Context, click *domain.SearchClick, windowSeconds int) (bool, error) {
					if click.Position != 3 {
						t.Errorf("expected position 3, got %d", click.Position)
					}
					if click.Confidence != domain.ConfidenceHigh {
						t.Errorf("expected confidence high, got %s", click.Confidence.String())
					}
					if windowSeconds != clickDedupWindowSeconds {
						t.Errorf("expected window %d, got %d", clickDedupWindowSeconds, windowSeconds)
					}
					if click.QueryNorm == "" {
						t.Error("expected non-empty QueryNorm")
					}
					if click.ResultSignature == "" {
						t.Error("expected non-empty ResultSignature")
					}
					return true, nil
				},
			},
			input: RecordClickInput{
				QueryNorm:      "radiohead creep",
				ResultKind:     domain.ResultKindTrack,
				ResultTitle:    "Creep",
				ResultSubtitle: "Radiohead",
				Position:       3,
				Confidence:     domain.ConfidenceHigh,
			},
			wantErr: false,
		},
		{
			name: "nil repo returns nil",
			repo: nil,
			input: RecordClickInput{
				QueryNorm:  "test",
				ResultKind: domain.ResultKindTrack,
				ResultTitle: "Test",
				Position:   0,
				Confidence: domain.ConfidenceLow,
			},
			wantErr: false,
			wantNil: true,
		},
		{
			name: "repo error propagates",
			repo: &fakeSearchClickRepository{
				insertIfOutsideWindowFn: func(_ context.Context, _ *domain.SearchClick, _ int) (bool, error) {
					return false, errors.New("db connection lost")
				},
			},
			input: RecordClickInput{
				QueryNorm:      "test query",
				ResultKind:     domain.ResultKindTrack,
				ResultTitle:    "Test Track",
				ResultSubtitle: "Test Artist",
				Position:       1,
				Confidence:     domain.ConfidenceLow,
			},
			wantErr: true,
		},
		{
			name: "dedup hit (not inserted) returns nil",
			repo: &fakeSearchClickRepository{
				insertIfOutsideWindowFn: func(_ context.Context, _ *domain.SearchClick, _ int) (bool, error) {
					return false, nil
				},
			},
			input: RecordClickInput{
				QueryNorm:      "duplicate click",
				ResultKind:     domain.ResultKindTrack,
				ResultTitle:    "Song",
				ResultSubtitle: "Artist",
				Position:       0,
				Confidence:     domain.ConfidenceLow,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var svc *RecordClickService
			if tt.wantNil {
				svc = NewRecordClickService(nil)
			} else {
				svc = NewRecordClickService(tt.repo)
			}

			err := svc.Execute(context.Background(), userID, tt.input)

			if tt.wantErr && err == nil {
				t.Error("expected error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no error, got %v", err)
			}
		})
	}
}
