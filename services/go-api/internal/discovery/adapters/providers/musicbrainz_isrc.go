package providers

import (
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

func (a *MusicBrainzAdapter) RecordingsByISRC(ctx context.Context, isrc string) ([]ports.ISRCRecording, error) {
	isrc = strings.ToUpper(strings.TrimSpace(isrc))
	if isrc == "" {
		return nil, nil
	}
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/isrc/%s?fmt=json", url.PathEscape(isrc))
	if err := a.limiter.wait(ctx); err != nil {
		return nil, err
	}
	status, rawBody, err := getBytes(ctx, a.client, u,
		withHeader("User-Agent", a.userAgent),
		withHeader("Accept", "application/json"))
	if status == http.StatusNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("musicbrainz isrc status %d: %w", status, err)
	}

	return parseISRCRecordings(rawBody)
}

func parseISRCRecordings(rawBody []byte) ([]ports.ISRCRecording, error) {
	var body mbRecordingResponse
	if err := json.Unmarshal(rawBody, &body); err != nil {
		return nil, err
	}
	out := make([]ports.ISRCRecording, 0, len(body.Recordings))
	for _, rec := range body.Recordings {
		if rec.ID == "" {
			continue
		}
		out = append(out, ports.ISRCRecording{MBID: rec.ID, Duration: rec.LengthMs / 1000})
	}
	return out, nil
}
