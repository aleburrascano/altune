package musicbrainz

import (
	"altune/go-api/internal/platform/outbound"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const msPerSecond = 1000.0

func (a *MusicBrainzAdapter) RecordingByMBID(ctx context.Context, mbid string) (ports.MusicBrainzRecording, bool, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if mbid == "" {
		return ports.MusicBrainzRecording{}, false, nil
	}
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/recording/%s?inc=isrcs+artist-credits&fmt=json", url.PathEscape(mbid))
	status, rawBody, err := outbound.GetBytes(ctx, a.client, u,
		outbound.WithHeader("User-Agent", a.userAgent),
		outbound.WithHeader("Accept", "application/json"))
	if status == http.StatusNotFound {
		return ports.MusicBrainzRecording{}, false, nil
	}
	if err != nil {
		return ports.MusicBrainzRecording{}, false, fmt.Errorf("musicbrainz recording status %d: %w", status, err)
	}
	return parseRecordingFacts(rawBody)
}

func parseRecordingFacts(rawBody []byte) (ports.MusicBrainzRecording, bool, error) {
	var rec mbRecording
	if err := json.Unmarshal(rawBody, &rec); err != nil {
		return ports.MusicBrainzRecording{}, false, fmt.Errorf("decode musicbrainz recording: %w", err)
	}
	if rec.ID == "" {
		return ports.MusicBrainzRecording{}, false, nil
	}
	facts := ports.MusicBrainzRecording{
		MBID:           rec.ID,
		Title:          rec.Title,
		Disambiguation: strings.TrimSpace(rec.Disambiguation),
		ISRCs:          rec.ISRCs,
		Duration:       float64(rec.LengthMs) / msPerSecond,
	}
	for _, credit := range rec.ArtistCredit {
		facts.Artists = append(facts.Artists, credit.Name)
	}
	return facts, true, nil
}
