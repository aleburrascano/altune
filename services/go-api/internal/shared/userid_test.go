package shared

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewUserId(t *testing.T) {
	raw := uuid.New()
	uid := NewUserId(raw)

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
			uid, err := ParseUserId(tt.input)
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
	uid, err := ParseUserId(raw)
	if err != nil {
		t.Fatalf("ParseUserId(%q): unexpected error: %v", raw, err)
	}

	got := uid.String()
	if got != raw {
		t.Errorf("String() round-trip: got %q, want %q", got, raw)
	}
}

func TestUserId_IsZero(t *testing.T) {
	tests := []struct {
		name string
		uid  UserId
		want bool
	}{
		{
			name: "nil UUID is zero",
			uid:  NewUserId(uuid.Nil),
			want: true,
		},
		{
			name: "zero-value struct is zero",
			uid:  UserId{},
			want: true,
		},
		{
			name: "valid UUID is not zero",
			uid:  NewUserId(uuid.New()),
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
