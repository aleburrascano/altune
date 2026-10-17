package musicbrainz

import (
	"altune/go-api/internal/discovery/adapters/providers/providerhttp"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
)

const (
	maxWorksPerRecording = 5
	performanceRelation  = "performance"
	liveAttribute        = "live"
)

type mbRecordingWorkRels struct {
	Relations []struct {
		Type string `json:"type"`
		Work *struct {
			ID string `json:"id"`
		} `json:"work"`
	} `json:"relations"`
}

type mbWorkRecordingRels struct {
	Relations []struct {
		TargetType string       `json:"target-type"`
		Attributes []string     `json:"attributes"`
		Recording  *mbRecording `json:"recording"`
	} `json:"relations"`
}

type mbRecordingDetailResponse struct {
	mbRecording
	FirstReleaseDate string `json:"first-release-date"`
	Releases         []struct {
		Title  string `json:"title"`
		Date   string `json:"date"`
		Status string `json:"status"`
	} `json:"releases"`
}

func (a *MusicBrainzAdapter) getMB(ctx context.Context, what, path string) (int, []byte, error) {
	if err := a.limiter.Wait(ctx); err != nil {
		return 0, nil, err
	}
	status, rawBody, err := providerhttp.GetBytes(ctx, a.client, musicbrainzAPIBaseURL+path,
		providerhttp.WithHeader("User-Agent", a.userAgent),
		providerhttp.WithHeader("Accept", "application/json"))
	if status == http.StatusNotFound {
		return status, nil, nil
	}
	if err != nil {
		return status, nil, fmt.Errorf("musicbrainz %s status %d: %w", what, status, err)
	}
	return status, rawBody, nil
}

func (a *MusicBrainzAdapter) WorkRecordings(ctx context.Context, recordingMBID string) ([]ports.MBWorkRecording, error) {
	recordingMBID = strings.ToLower(strings.TrimSpace(recordingMBID))
	if recordingMBID == "" {
		return nil, nil
	}
	workIDs, err := a.performanceWorkIDs(ctx, recordingMBID)
	if err != nil {
		return nil, err
	}
	var out []ports.MBWorkRecording
	seen := map[string]bool{recordingMBID: true}
	for _, workID := range workIDs {
		found, err := a.recordingsOfWork(ctx, workID)
		if err != nil {
			return nil, err
		}
		out = appendUnseen(out, seen, found)
	}
	return out, nil
}

func (a *MusicBrainzAdapter) performanceWorkIDs(ctx context.Context, recordingMBID string) ([]string, error) {
	var rec mbRecordingWorkRels
	found, err := a.getMBJSON(ctx, "recording work-rels",
		fmt.Sprintf("/recording/%s?inc=work-rels+artist-credits&fmt=json", url.PathEscape(recordingMBID)), &rec)
	if err != nil || !found {
		return nil, err
	}
	var workIDs []string
	for _, rel := range rec.Relations {
		if rel.Type != performanceRelation || rel.Work == nil || rel.Work.ID == "" || slices.Contains(workIDs, rel.Work.ID) {
			continue
		}
		workIDs = append(workIDs, rel.Work.ID)
	}
	if len(workIDs) > maxWorksPerRecording {
		workIDs = workIDs[:maxWorksPerRecording]
	}
	return workIDs, nil
}

func appendUnseen(out []ports.MBWorkRecording, seen map[string]bool, found []ports.MBWorkRecording) []ports.MBWorkRecording {
	for _, r := range found {
		if seen[r.MBID] {
			continue
		}
		seen[r.MBID] = true
		out = append(out, r)
	}
	return out
}

func (a *MusicBrainzAdapter) getMBJSON(ctx context.Context, what, path string, dst any) (bool, error) {
	status, rawBody, err := a.getMB(ctx, what, path)
	if err != nil {
		return false, err
	}
	if status == http.StatusNotFound {
		return false, nil
	}
	if err := json.Unmarshal(rawBody, dst); err != nil {
		return false, fmt.Errorf("musicbrainz %s: %w", what, err)
	}
	return true, nil
}

func (a *MusicBrainzAdapter) recordingsOfWork(ctx context.Context, workID string) ([]ports.MBWorkRecording, error) {
	var work mbWorkRecordingRels
	found, err := a.getMBJSON(ctx, "work recording-rels",
		fmt.Sprintf("/work/%s?inc=recording-rels+artist-credits&fmt=json", url.PathEscape(workID)), &work)
	if err != nil || !found {
		return nil, err
	}
	var out []ports.MBWorkRecording
	for _, rel := range work.Relations {
		if rel.TargetType != "recording" || rel.Recording == nil || rel.Recording.ID == "" {
			continue
		}
		out = append(out, workRecording(rel.Recording, slices.Contains(rel.Attributes, liveAttribute)))
	}
	return out, nil
}

func workRecording(rec *mbRecording, live bool) ports.MBWorkRecording {
	return ports.MBWorkRecording{
		MBID:           strings.ToLower(rec.ID),
		Title:          rec.Title,
		Disambiguation: strings.TrimSpace(rec.Disambiguation),
		ArtistIDs:      artistIDs(rec.ArtistCredit),
		Duration:       float64(rec.LengthMs) / msPerSecond,
		Live:           live,
	}
}

func (a *MusicBrainzAdapter) RecordingDetail(ctx context.Context, mbid string) (ports.MBRecordingDetail, bool, error) {
	mbid = strings.ToLower(strings.TrimSpace(mbid))
	if mbid == "" {
		return ports.MBRecordingDetail{}, false, nil
	}
	var rec mbRecordingDetailResponse
	found, err := a.getMBJSON(ctx, "recording detail",
		fmt.Sprintf("/recording/%s?inc=isrcs+artist-credits+releases&fmt=json", url.PathEscape(mbid)), &rec)
	if err != nil || !found || rec.ID == "" {
		return ports.MBRecordingDetail{}, false, err
	}
	return recordingDetail(rec), true, nil
}

func recordingDetail(rec mbRecordingDetailResponse) ports.MBRecordingDetail {
	detail := ports.MBRecordingDetail{
		MBID:             rec.ID,
		Title:            rec.Title,
		ArtistIDs:        artistIDs(rec.ArtistCredit),
		ISRCs:            rec.ISRCs,
		Duration:         float64(rec.LengthMs) / msPerSecond,
		FirstReleaseDate: rec.FirstReleaseDate,
	}
	for _, r := range rec.Releases {
		detail.Releases = append(detail.Releases, ports.MBRelease{Title: r.Title, Date: r.Date, Status: r.Status})
	}
	return detail
}

func artistIDs(credits []mbArtistRef) []string {
	var ids []string
	for _, c := range credits {
		if c.Artist != nil && c.Artist.ID != "" {
			ids = append(ids, c.Artist.ID)
		}
	}
	return ids
}
