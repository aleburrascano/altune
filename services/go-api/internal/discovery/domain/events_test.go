package domain

import "testing"

func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   EventType
		want string
	}{
		{EventTypeSearchPerformed, "search_performed"},
		{EventTypeResultsShown, "results_shown"},
		{EventTypeResultClicked, "result_clicked"},
		{EventTypePlay, "play"},
		{EventTypeSkip, "skip"},
		{EventTypeCompleted, "completed"},
		{EventTypeLibraryAdd, "library_add"},
		{EventTypeWrongAlbum, "wrong_album"},
		{EventTypeUnknown, "unknown"},
		{EventType(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.et.String()
			if got != tt.want {
				t.Errorf("EventType(%d).String() = %q, want %q", tt.et, got, tt.want)
			}
		})
	}
}

func TestParseEventType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  EventType
	}{
		{name: "search_performed", input: "search_performed", want: EventTypeSearchPerformed},
		{name: "results_shown", input: "results_shown", want: EventTypeResultsShown},
		{name: "result_clicked", input: "result_clicked", want: EventTypeResultClicked},
		{name: "play", input: "play", want: EventTypePlay},
		{name: "skip", input: "skip", want: EventTypeSkip},
		{name: "completed", input: "completed", want: EventTypeCompleted},
		{name: "library_add", input: "library_add", want: EventTypeLibraryAdd},
		{name: "wrong_album", input: "wrong_album", want: EventTypeWrongAlbum},
		{name: "invalid", input: "page_view", want: EventTypeUnknown},
		{name: "empty", input: "", want: EventTypeUnknown},
		{name: "uppercase rejected", input: "Play", want: EventTypeUnknown},
		// "unknown" is the sentinel's String() output, not a wire value — it must
		// NOT parse to a valid type.
		{name: "sentinel name rejected", input: "unknown", want: EventTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseEventType(tt.input)
			if got != tt.want {
				t.Errorf("ParseEventType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEventType_RoundTrip(t *testing.T) {
	// Every named EventType should survive String() -> Parse() round-trip.
	types := []EventType{
		EventTypeSearchPerformed, EventTypeResultsShown, EventTypeResultClicked,
		EventTypePlay, EventTypeSkip, EventTypeCompleted,
		EventTypeLibraryAdd, EventTypeWrongAlbum,
	}
	for _, et := range types {
		t.Run(et.String(), func(t *testing.T) {
			parsed := ParseEventType(et.String())
			if parsed != et {
				t.Errorf("round-trip: got %v, want %v", parsed, et)
			}
		})
	}
}

func TestEventType_ClientSubmittable(t *testing.T) {
	// Full matrix: only the interaction types are client-allowed; the
	// server-emitted envelope events (search_performed, results_shown) and the
	// unknown sentinel must be rejected at the POST /events boundary.
	tests := []struct {
		et   EventType
		want bool
	}{
		{EventTypeUnknown, false},
		{EventTypeSearchPerformed, false},
		{EventTypeResultsShown, false},
		{EventTypeResultClicked, true},
		{EventTypePlay, true},
		{EventTypeSkip, true},
		{EventTypeCompleted, true},
		{EventTypeLibraryAdd, true},
		{EventTypeWrongAlbum, true},
		{EventType(999), false},
	}

	for _, tt := range tests {
		t.Run(tt.et.String(), func(t *testing.T) {
			got := tt.et.ClientSubmittable()
			if got != tt.want {
				t.Errorf("EventType(%d).ClientSubmittable() = %v, want %v", tt.et, got, tt.want)
			}
		})
	}
}
