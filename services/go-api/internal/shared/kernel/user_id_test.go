package kernel_test

import (
	"altune/go-api/internal/shared/kernel"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNewUserId(t *testing.T) {
	raw := uuid.New()
	uid := kernel.NewUserId(raw)

	if uid.UUID() != raw {
		t.Errorf("NewUserId round-trip: got UUID()=%v, want %v", uid.UUID(), raw)
	}
}

func TestParseUserId(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{
			name:    "valid UUID",
			input:   "550e8400-e29b-41d4-a716-446655440000",
			wantErr: false,
		},
		{
			name:    "invalid string",
			input:   "not-a-uuid",
			wantErr: true,
		},
		{
			name:    "empty string",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			uid, err := kernel.ParseUserId(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("ParseUserId(%q): expected error, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUserId(%q): unexpected error: %v", tt.input, err)
			}
			if uid.String() != tt.input {
				t.Errorf("ParseUserId(%q): String()=%q, want %q", tt.input, uid.String(), tt.input)
			}
		})
	}
}

func TestUserId_String(t *testing.T) {
	const raw = "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	uid, err := kernel.ParseUserId(raw)
	if err != nil {
		t.Fatalf("ParseUserId(%q): unexpected error: %v", raw, err)
	}

	got := uid.String()
	if got != raw {
		t.Errorf("String() round-trip: got %q, want %q", got, raw)
	}
}

func TestGuardNotSystem(t *testing.T) {
	tests := []struct {
		name       string
		uid        kernel.UserId
		wantReject bool
	}{
		{
			name:       "the system identity is rejected",
			uid:        kernel.SystemUserId(),
			wantReject: true,
		},
		{
			name:       "a real user is allowed",
			uid:        kernel.NewUserId(uuid.New()),
			wantReject: false,
		},
		{
			name:       "the zero user id is not the system identity",
			uid:        kernel.UserId{},
			wantReject: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := kernel.GuardNotSystem(tt.uid)

			if !tt.wantReject {
				if err != nil {
					t.Fatalf("GuardNotSystem(%v): unexpected error: %v", tt.uid, err)
				}
				return
			}
			if !errors.Is(err, kernel.ErrSystemUserPersonalization) {
				t.Fatalf("GuardNotSystem(%v)=%v, want ErrSystemUserPersonalization", tt.uid, err)
			}
		})
	}
}

func TestUserId_IsZero(t *testing.T) {
	tests := []struct {
		name string
		uid  kernel.UserId
		want bool
	}{
		{
			name: "nil UUID is zero",
			uid:  kernel.NewUserId(uuid.Nil),
			want: true,
		},
		{
			name: "zero-value struct is zero",
			uid:  kernel.UserId{},
			want: true,
		},
		{
			name: "valid UUID is not zero",
			uid:  kernel.NewUserId(uuid.New()),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.uid.IsZero(); got != tt.want {
				t.Errorf("IsZero()=%v, want %v", got, tt.want)
			}
		})
	}
}
