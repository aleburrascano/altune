package handler_test

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/jsoncontract"
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	wireTrackID    = "00000000-0000-4000-8000-000000000001"
	wireTrackIDTwo = "00000000-0000-4000-8000-000000000002"
	wirePlaylistID = "5d1c3f0e-6f4e-4f55-9a4e-0d6a2c0c9b11"
)

func wirePayloads() []events.Payload {
	return []events.Payload{
		events.TrackDeleted{TrackID: wireTrackID},
		events.TrackAcquisitionStarted{TrackID: wireTrackID, Title: "Paranoid Android", Artist: "Radiohead"},
		events.TrackAcquisitionProgress{TrackID: wireTrackID, Stage: "tag", Title: "Paranoid Android", Artist: "Radiohead"},
		events.TrackAcquisitionCompleted{TrackID: wireTrackID, AudioRef: "audio/ref.m4a"},
		events.TrackAcquisitionFailed{TrackID: wireTrackID, Reason: "no_source"},
		events.TrackReplaceFailed{TrackID: wireTrackID, Reason: "no_source"},
		events.TrackAddedToPlaylist{PlaylistID: wirePlaylistID, TrackID: wireTrackID},
		events.TracksAddedToPlaylist{PlaylistID: wirePlaylistID, TrackIDs: []string{wireTrackID, wireTrackIDTwo}},
		events.TrackRemovedFromPlaylist{PlaylistID: wirePlaylistID, TrackID: wireTrackID},
		events.TracksRemovedFromPlaylist{PlaylistID: wirePlaylistID, TrackIDs: []string{wireTrackID, wireTrackIDTwo}},
		events.PlaylistCreated{PlaylistID: wirePlaylistID, Name: "Road trip"},
		events.PlaylistDeleted{PlaylistID: wirePlaylistID},
		events.PlaylistRenamed{PlaylistID: wirePlaylistID, Name: "Road trip"},
		events.PlaylistReordered{PlaylistID: wirePlaylistID, TrackIDs: []string{wireTrackIDTwo, wireTrackID}},
	}
}

type sseFrame struct {
	event string
	data  string
}

func readFrame(t *testing.T, r *bufio.Reader) sseFrame {
	t.Helper()
	deadline := time.After(5 * time.Second)
	var frame sseFrame
	for {
		line := strings.TrimRight(readLineBefore(t, r, deadline), "\n")
		switch {
		case line == "" && frame.event != "":
			return frame
		case strings.HasPrefix(line, "event: "):
			frame.event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			frame.data = strings.TrimPrefix(line, "data: ")
		}
	}
}

func openWireStream(t *testing.T, srvURL, lastEventID string) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srvURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	if lastEventID != "" {
		req.Header.Set("Last-Event-ID", lastEventID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return bufio.NewReader(resp.Body)
}

func TestWire_EventFramesCarryEveryPublishedType(t *testing.T) {
	bus := events.NewInProcessBus(context.Background(), time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)
	stream := openWireStream(t, srv.URL, "")
	readUntil(t, stream, func(l string) bool { return strings.HasPrefix(l, ":") })

	payloads := wirePayloads()
	for _, payload := range payloads {
		bus.Publish(context.Background(), uid, payload)
	}

	got := make(map[string]string, len(payloads)+1)
	for range payloads {
		frame := readFrame(t, stream)
		got[frame.event] = frame.data
	}
	for _, payload := range payloads {
		name := string(payload.EventType())
		data, ok := got[name]
		if !ok {
			t.Errorf("no %s frame arrived", name)
			continue
		}
		jsoncontract.AssertWire(t, "events_"+name, []byte(data))
	}

	resync := readResyncFrame(t, srv.URL)
	jsoncontract.AssertWire(t, "events_resync", []byte(resync.data))

	names := []string{resync.event}
	for _, payload := range payloads {
		names = append(names, string(payload.EventType()))
	}
	names = append(names, string(events.TypeTrackAddedToLibrary))
	sort.Strings(names)
	list, err := json.Marshal(names)
	if err != nil {
		t.Fatal(err)
	}
	jsoncontract.AssertWireExact(t, "events_types", list)
}

func readResyncFrame(t *testing.T, srvURL string) sseFrame {
	t.Helper()
	return readFrame(t, openWireStream(t, srvURL, "not-an-id"))
}
