package events

import (
	"encoding/json"
	"strings"
	"testing"
)

type publishedType struct {
	constant string
	value    EventType
	wire     string
}

var publishedTypes = []publishedType{
	{"TypeTrackAddedToLibrary", TypeTrackAddedToLibrary, "track_added_to_library"},
	{"TypeTrackDeleted", TypeTrackDeleted, "track_deleted"},
	{"TypeTrackAcquisitionStarted", TypeTrackAcquisitionStarted, "track_acquisition_started"},
	{"TypeTrackAcquisitionProgress", TypeTrackAcquisitionProgress, "track_acquisition_progress"},
	{"TypeTrackAcquisitionCompleted", TypeTrackAcquisitionCompleted, "track_acquisition_completed"},
	{"TypeTrackAcquisitionFailed", TypeTrackAcquisitionFailed, "track_acquisition_failed"},
	{"TypeTrackReplaceFailed", TypeTrackReplaceFailed, "track_replace_failed"},
	{"TypeTrackAddedToPlaylist", TypeTrackAddedToPlaylist, "track_added_to_playlist"},
	{"TypeTracksAddedToPlaylist", TypeTracksAddedToPlaylist, "tracks_added_to_playlist"},
	{"TypeTrackRemovedFromPlaylist", TypeTrackRemovedFromPlaylist, "track_removed_from_playlist"},
	{"TypeTracksRemovedFromPlaylist", TypeTracksRemovedFromPlaylist, "tracks_removed_from_playlist"},
	{"TypePlaylistCreated", TypePlaylistCreated, "playlist_created"},
	{"TypePlaylistDeleted", TypePlaylistDeleted, "playlist_deleted"},
	{"TypePlaylistRenamed", TypePlaylistRenamed, "playlist_renamed"},
	{"TypePlaylistReordered", TypePlaylistReordered, "playlist_reordered"},
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
	definedBy := make(map[EventType]string, len(publishedTypes))
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
	raw, err := json.Marshal(Event{Type: TypeTrackDeleted})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"track_deleted"`) {
		t.Errorf("marshalled event = %s, want it to carry \"type\":\"track_deleted\"", raw)
	}
}
