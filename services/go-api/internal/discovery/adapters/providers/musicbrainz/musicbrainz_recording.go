package musicbrainz

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const msPerSecond = 1000.0

func (a *MusicBrainzAdapter) RecordingByMBID(ctx context.Context, mbid string) (ports.MBRecording, bool, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if mbid == "" {
		return ports.MBRecording{}, false, nil
	}
	u := fmt.Sprintf(musicbrainzAPIBaseURL+"/recording/%s?inc=isrcs+artist-credits&fmt=json", url.PathEscape(mbid))
	if err := a.limiter.Wait(ctx); err != nil {
		return ports.MBRecording{}, false, err
	}
	status, rawBody, err := providerhttp.GetBytes(ctx, a.client, u,
		providerhttp.WithHeader("User-Agent", a.userAgent),
		providerhttp.WithHeader("Accept", "application/json"))
	if status == http.StatusNotFound {
		return ports.MBRecording{}, false, nil
	}
	if err != nil {
		return ports.MBRecording{}, false, fmt.Errorf("musicbrainz recording status %d: %w", status, err)
	}
	return parseRecordingFacts(rawBody)
}

func parseRecordingFacts(rawBody []byte) (ports.MBRecording, bool, error) {
	var rec mbRecording
	if err := json.Unmarshal(rawBody, &rec); err != nil {
		return ports.MBRecording{}, false, err
	}
	if rec.ID == "" {
		return ports.MBRecording{}, false, nil
	}
	facts := ports.MBRecording{
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
