package events_test

import (
	"altune/go-api/internal/shared/events"
	"encoding/json"
	"reflect"
	"testing"
)

func TestPayloads_KeepTheWireShapeClientsParse(t *testing.T) {
	cases := []struct {
		payload events.Payload
		want    events.EventType
		json    string
	}{
		{events.TrackDeleted{TrackID: "t1"}, "track_deleted", `{"track_id":"t1"}`},
		{events.TrackAcquisitionStarted{TrackID: "t1", Title: "T", Artist: "A"}, "track_acquisition_started", `{"track_id":"t1","title":"T","artist":"A"}`},
		{events.TrackAcquisitionProgress{TrackID: "t1", Stage: "download", Title: "T", Artist: "A"}, "track_acquisition_progress", `{"track_id":"t1","stage":"download","title":"T","artist":"A"}`},
		{events.TrackAcquisitionCompleted{TrackID: "t1", AudioRef: "r"}, "track_acquisition_completed", `{"track_id":"t1","audio_ref":"r"}`},
		{events.TrackAcquisitionFailed{TrackID: "t1", Reason: "x"}, "track_acquisition_failed", `{"track_id":"t1","reason":"x"}`},
		{events.TrackReplaceFailed{TrackID: "t1", Reason: "x"}, "track_replace_failed", `{"track_id":"t1","reason":"x"}`},
		{events.TrackAddedToPlaylist{PlaylistID: "p", TrackID: "t1"}, "track_added_to_playlist", `{"playlist_id":"p","track_id":"t1"}`},
		{events.TrackRemovedFromPlaylist{PlaylistID: "p", TrackID: "t1"}, "track_removed_from_playlist", `{"playlist_id":"p","track_id":"t1"}`},
		{events.TracksAddedToPlaylist{PlaylistID: "p", TrackIDs: []string{"a", "b"}}, "tracks_added_to_playlist", `{"playlist_id":"p","track_ids":["a","b"]}`},
		{events.TracksRemovedFromPlaylist{PlaylistID: "p", TrackIDs: []string{"a"}}, "tracks_removed_from_playlist", `{"playlist_id":"p","track_ids":["a"]}`},
		{events.PlaylistReordered{PlaylistID: "p", TrackIDs: []string{"b", "a"}}, "playlist_reordered", `{"playlist_id":"p","track_ids":["b","a"]}`},
		{events.PlaylistCreated{PlaylistID: "p", Name: "N"}, "playlist_created", `{"playlist_id":"p","name":"N"}`},
		{events.PlaylistDeleted{PlaylistID: "p"}, "playlist_deleted", `{"playlist_id":"p"}`},
		{events.PlaylistRenamed{PlaylistID: "p", Name: "N"}, "playlist_renamed", `{"playlist_id":"p","name":"N"}`},
		{events.TrackAddedToLibrary{TrackID: "t1", Track: json.RawMessage(`{"id":"t1","title":"T"}`)}, "track_added_to_library", `{"id":"t1","title":"T","track_id":"t1"}`},
	}
	seen := map[events.EventType]bool{}
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			if got := tc.payload.EventType(); got != tc.want {
				t.Errorf("EventType = %q, want %q", got, tc.want)
			}
			raw, err := json.Marshal(tc.payload)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			if !jsonEqual(t, string(raw), tc.json) {
				t.Errorf("wire = %s, want %s", raw, tc.json)
			}
		})
		seen[tc.want] = true
	}
	for _, pt := range publishedTypes {
		if !seen[pt.value] {
			t.Errorf("%s has no typed payload covered here", pt.constant)
		}
	}
}

func TestPayloads_NameTheSubjectTheTapShows(t *testing.T) {
	withSubject := []events.Payload{
		events.TrackDeleted{TrackID: "t1"},
		events.TrackAcquisitionStarted{TrackID: "t1"},
		events.TrackAcquisitionProgress{TrackID: "t1"},
		events.TrackAcquisitionCompleted{TrackID: "t1"},
		events.TrackAcquisitionFailed{TrackID: "t1"},
		events.TrackReplaceFailed{TrackID: "t1"},
		events.TrackAddedToPlaylist{TrackID: "t1"},
		events.TrackRemovedFromPlaylist{TrackID: "t1"},
		events.TrackAddedToLibrary{TrackID: "t1"},
	}
	for _, p := range withSubject {
		s, ok := p.(events.Subjected)
		if !ok || s.Subject() != "t1" {
			t.Errorf("%T subject = %v, want t1", p, ok)
		}
	}
	if _, ok := events.Payload(events.PlaylistCreated{}).(events.Subjected); ok {
		t.Error("a playlist event names a subject, but the tap shows only track subjects")
	}
}

func TestTrackAddedToLibrary_RefusesAnUnmarshalableTrack(t *testing.T) {
	_, err := json.Marshal(events.TrackAddedToLibrary{TrackID: "t1"})
	if err == nil {
		t.Fatal("marshal succeeded, want the error so the stream resyncs instead of sending a half frame")
	}
}

func TestTrackAddedToLibrary_RefusesATrackThatIsNotAnObject(t *testing.T) {
	_, err := json.Marshal(events.TrackAddedToLibrary{TrackID: "t1", Track: json.RawMessage(`["x"]`)})
	if err == nil {
		t.Fatal("marshal succeeded for a non-object track")
	}
}

func jsonEqual(t *testing.T, a, b string) bool {
	t.Helper()
	var x, y any
	if err := json.Unmarshal([]byte(a), &x); err != nil {
		t.Fatalf("unmarshal %s: %v", a, err)
	}
	if err := json.Unmarshal([]byte(b), &y); err != nil {
		t.Fatalf("unmarshal %s: %v", b, err)
	}
	return reflect.DeepEqual(x, y)
}
