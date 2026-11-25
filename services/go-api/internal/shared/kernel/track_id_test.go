package kernel_test

import (
	"altune/go-api/internal/shared/kernel"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestParseTrackId(t *testing.T) {
	t.Parallel()
	validUUID := "550e8400-e29b-41d4-a716-446655440000"

	tests := []struct {
		name    string
		input   string
		wantErr bool
		wantStr string
	}{
		{name: "valid UUID", input: validUUID, wantStr: validUUID},
		{name: "invalid UUID", input: "not-a-uuid", wantErr: true},
		{name: "empty string", input: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := kernel.ParseTrackId(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for input %q, got nil", tt.input)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := id.String(); got != tt.wantStr {
				t.Errorf("String() = %q, want %q", got, tt.wantStr)
			}
		})
	}
}

func TestTrackIdRoundTrip(t *testing.T) {
	t.Parallel()
	id := kernel.NewTrackId()
	got, err := kernel.ParseTrackId(id.String())
	if err != nil {
		t.Fatalf("ParseTrackId(%q) error = %v", id.String(), err)
	}
	if got != id {
		t.Errorf("ParseTrackId(%q) = %v, want %v", id.String(), got, id)
	}
}

func TestTrackIdFromUUID(t *testing.T) {
	t.Parallel()
	raw := uuid.New()
	id := kernel.TrackIdFromUUID(raw)

	if id.UUID() != raw {
		t.Errorf("UUID() = %v, want %v", id.UUID(), raw)
	}
	if id.String() != raw.String() {
		t.Errorf("String() = %q, want %q", id.String(), raw.String())
	}
}

func TestTrackIdFromUUIDMatchesParse(t *testing.T) {
	t.Parallel()
	raw := uuid.New()
	id := kernel.TrackIdFromUUID(raw)
	parsed, err := kernel.ParseTrackId(raw.String())
	if err != nil {
		t.Fatal(err)
	}
	if parsed != id {
		t.Errorf("ParseTrackId(%q) = %v, want %v", raw, parsed, id)
	}
}

func TestTrackId_IsZero(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		id   kernel.TrackId
		want bool
	}{
		{name: "nil UUID is zero", id: kernel.TrackIdFromUUID(uuid.Nil), want: true},
		{name: "non-nil UUID is not zero", id: kernel.TrackIdFromUUID(uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")), want: false},
		{name: "default value is zero", id: kernel.TrackId{}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.id.IsZero(); got != tt.want {
				t.Errorf("IsZero() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrackIdJSONUnchanged(t *testing.T) {
	t.Parallel()
	raw, err := json.Marshal(struct {
		ID kernel.TrackId `json:"ID"`
	}{})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(raw) != `{"ID":{}}` {
		t.Errorf("json.Marshal(TrackId) = %s, want {\"ID\":{}}", raw)
	}
}

func TestTrackIdIsMapKey(t *testing.T) {
	t.Parallel()
	a, b := kernel.NewTrackId(), kernel.NewTrackId()
	m := map[kernel.TrackId]string{a: "a", b: "b"}
	if m[a] != "a" || m[b] != "b" || len(m) != 2 {
		t.Errorf("TrackId map lookup failed: %v", m)
	}
}
