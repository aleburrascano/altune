package events_test

import (
	"altune/go-api/internal/shared/events"
	"encoding/json"
	"strings"
	"testing"
)

type publishedType struct {
	constant string
	value    events.EventType
	wire     string
}

var publishedTypes = []publishedType{
	{"TypeTrackAddedToLibrary", events.TypeTrackAddedToLibrary, "track_added_to_library"},
	{"TypeTrackDeleted", events.TypeTrackDeleted, "track_deleted"},
	{"TypeTrackAcquisitionStarted", events.TypeTrackAcquisitionStarted, "track_acquisition_started"},
	{"TypeTrackAcquisitionProgress", events.TypeTrackAcquisitionProgress, "track_acquisition_progress"},
	{"TypeTrackAcquisitionCompleted", events.TypeTrackAcquisitionCompleted, "track_acquisition_completed"},
	{"TypeTrackAcquisitionFailed", events.TypeTrackAcquisitionFailed, "track_acquisition_failed"},
	{"TypeTrackReplaceFailed", events.TypeTrackReplaceFailed, "track_replace_failed"},
	{"TypeTrackAddedToPlaylist", events.TypeTrackAddedToPlaylist, "track_added_to_playlist"},
	{"TypeTracksAddedToPlaylist", events.TypeTracksAddedToPlaylist, "tracks_added_to_playlist"},
	{"TypeTrackRemovedFromPlaylist", events.TypeTrackRemovedFromPlaylist, "track_removed_from_playlist"},
	{"TypeTracksRemovedFromPlaylist", events.TypeTracksRemovedFromPlaylist, "tracks_removed_from_playlist"},
	{"TypePlaylistCreated", events.TypePlaylistCreated, "playlist_created"},
	{"TypePlaylistDeleted", events.TypePlaylistDeleted, "playlist_deleted"},
	{"TypePlaylistRenamed", events.TypePlaylistRenamed, "playlist_renamed"},
	{"TypePlaylistReordered", events.TypePlaylistReordered, "playlist_reordered"},
}

func TestEventTypes_CarryTheWireValuesClientsSubscribeTo(t *testing.T) {
	for _, pt := range publishedTypes {
		if string(pt.value) != pt.wire {
			t.Errorf("%s = %q, want %q: deployed clients subscribe to the wanted value",
				pt.constant, pt.value, pt.wire)
		}
	}
}

func TestEventTypes_NoTwoEventTypesShareAWireValue(t *testing.T) {
	definedBy := make(map[events.EventType]string, len(publishedTypes))
	for _, pt := range publishedTypes {
		if first, taken := definedBy[pt.value]; taken {
			t.Errorf("%s and %s both publish %q; a subscriber cannot tell the two apart",
				first, pt.constant, pt.value)
			continue
		}
		definedBy[pt.value] = pt.constant
	}
}

func TestEvent_MarshalsTheTypedNameAsTheBareWireString(t *testing.T) {
	raw, err := json.Marshal(events.Event{Type: events.TypeTrackDeleted})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"track_deleted"`) {
		t.Errorf("marshalled event = %s, want it to carry \"type\":\"track_deleted\"", raw)
	}
}
