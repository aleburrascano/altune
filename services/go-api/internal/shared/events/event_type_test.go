package events_test

import (
	"altune/go-api/internal/shared/events"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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
	{"TypeTrackDeleted", events.TrackDeleted{}.EventType(), "track_deleted"},
	{"TypeTrackAcquisitionStarted", events.TypeTrackAcquisitionStarted, "track_acquisition_started"},
	{"TypeTrackAcquisitionProgress", events.TypeTrackAcquisitionProgress, "track_acquisition_progress"},
	{"TypeTrackAcquisitionCompleted", events.TrackAcquisitionCompleted{}.EventType(), "track_acquisition_completed"},
	{"TypeTrackAcquisitionFailed", events.TypeTrackAcquisitionFailed, "track_acquisition_failed"},
	{"TypeTrackReplaceFailed", events.TypeTrackReplaceFailed, "track_replace_failed"},
	{"TypeTrackAddedToPlaylist", events.TrackAddedToPlaylist{}.EventType(), "track_added_to_playlist"},
	{"TypeTracksAddedToPlaylist", events.TracksAddedToPlaylist{}.EventType(), "tracks_added_to_playlist"},
	{"TypeTrackRemovedFromPlaylist", events.TrackRemovedFromPlaylist{}.EventType(), "track_removed_from_playlist"},
	{"TypeTracksRemovedFromPlaylist", events.TracksRemovedFromPlaylist{}.EventType(), "tracks_removed_from_playlist"},
	{"TypePlaylistCreated", events.PlaylistCreated{}.EventType(), "playlist_created"},
	{"TypePlaylistDeleted", events.PlaylistDeleted{}.EventType(), "playlist_deleted"},
	{"TypePlaylistRenamed", events.TypePlaylistRenamed, "playlist_renamed"},
	{"TypePlaylistReordered", events.PlaylistReordered{}.EventType(), "playlist_reordered"},
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
	raw, err := json.Marshal(events.Event{Type: events.TrackDeleted{}.EventType()})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(raw), `"type":"track_deleted"`) {
		t.Errorf("marshalled event = %s, want it to carry \"type\":\"track_deleted\"", raw)
	}
}

func declaredEventTypes(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), "event_type.go", nil, 0)
	if err != nil {
		t.Fatalf("parse event_type.go: %v", err)
	}
	var values []string
	ast.Inspect(file, func(n ast.Node) bool {
		spec, ok := n.(*ast.ValueSpec)
		if !ok || spec.Type == nil || len(spec.Values) != 1 {
			return true
		}
		if ident, ok := spec.Type.(*ast.Ident); !ok || ident.Name != "EventType" {
			return true
		}
		lit, ok := spec.Values[0].(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		value, err := strconv.Unquote(lit.Value)
		if err != nil {
			t.Fatalf("unquote %s: %v", lit.Value, err)
		}
		values = append(values, value)
		return true
	})
	return values
}

func TestEveryDeclaredEventTypeHasAWireFixture(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "contracts", "wire", "events_types.json"))
	if err != nil {
		t.Fatalf("read the published event types fixture: %v", err)
	}
	var published []string
	if err := json.Unmarshal(raw, &published); err != nil {
		t.Fatalf("decode the published event types fixture: %v", err)
	}

	declared := declaredEventTypes(t)

	if len(declared) == 0 {
		t.Fatal("found no EventType constants in event_type.go, so this test checks nothing")
	}
	for _, eventType := range declared {
		if !slices.Contains(published, eventType) {
			t.Errorf("event type %q is declared in event_type.go but has no payload in the wire contract test, so the client is not checked against it: add it to wirePayloads in platform/eventstream and run scripts/update-wire-contracts.sh", eventType)
		}
	}
}
