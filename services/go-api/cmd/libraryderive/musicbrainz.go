package main

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
)

const (
	musicBrainzBaseURL     = "https://musicbrainz.org"
	musicBrainzUserAgent   = "altune-libraryderive/1.0 ( https://github.com/aleburrascano/altune )"
	musicBrainzRequestsSec = 1.0
	musicBrainzResultLimit = 25
	musicBrainzPageSize    = 100
	millisPerSecond        = 1000.0
)

type recording struct {
	ID      string
	Title   string
	Artist  string
	Primary string
	ISRCs   []string
}

func (r recording) hasISRC(isrc string) bool {
	return slices.ContainsFunc(r.ISRCs, func(listed string) bool { return canonicalISRC(listed) == canonicalISRC(isrc) })
}

type recordingSource interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error)
	Recording(ctx context.Context, id string) (recording, error)
	ReleaseGroups(ctx context.Context, recordingID string) ([]releaseGroup, error)
}

type musicBrainz struct{ client *catalogClient }

func newMusicBrainz(httpClient *http.Client, baseURL string, requestsPerSecond float64) musicBrainz {
	return musicBrainz{client: newCatalogClient(httpClient, baseURL, musicBrainzUserAgent, requestsPerSecond, isMusicBrainzThrottle)}
}

func isMusicBrainzThrottle(status int, _ []byte) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

type artistCredit []struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
}

func (c artistCredit) primary() string {
	if len(c) == 0 {
		return ""
	}
	return c[0].Name
}

func (c artistCredit) text() string {
	var out strings.Builder
	for _, credit := range c {
		out.WriteString(credit.Name + credit.JoinPhrase)
	}
	return strings.TrimSpace(out.String())
}

type recordingBody struct {
	ID           string       `json:"id"`
	Title        string       `json:"title"`
	ISRCs        []string     `json:"isrcs"`
	ArtistCredit artistCredit `json:"artist-credit"`
}

type recordingSearchBody struct {
	Recordings []recordingBody `json:"recordings"`
}

type releaseGroupBody struct {
	ID               string       `json:"id"`
	Title            string       `json:"title"`
	PrimaryType      string       `json:"primary-type"`
	SecondaryTypes   []string     `json:"secondary-types"`
	FirstReleaseDate string       `json:"first-release-date"`
	ArtistCredit     artistCredit `json:"artist-credit"`
}

type releasePageBody struct {
	ReleaseCount int `json:"release-count"`
	Releases     []struct {
		ArtistCredit artistCredit     `json:"artist-credit"`
		ReleaseGroup releaseGroupBody `json:"release-group"`
	} `json:"releases"`
}

func (m musicBrainz) RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error) {
	query := `isrc:"` + canonicalISRC(isrc) + `"`
	path := "/ws/2/recording?query=" + url.QueryEscape(query) + "&inc=isrcs&fmt=json&limit=" + strconv.Itoa(musicBrainzResultLimit)
	var body recordingSearchBody
	if err := m.getJSON(ctx, path, &body); err != nil {
		return nil, err
	}
	found := make([]recording, 0, len(body.Recordings))
	for _, item := range body.Recordings {
		found = append(found, toRecording(item))
	}
	return found, nil
}

func (m musicBrainz) Recording(ctx context.Context, id string) (recording, error) {
	path := "/ws/2/recording/" + url.PathEscape(id) + "?inc=isrcs+artist-credits&fmt=json"
	var body recordingBody
	if err := m.getJSON(ctx, path, &body); err != nil {
		return recording{}, err
	}
	return toRecording(body), nil
}

func (m musicBrainz) ReleaseGroups(ctx context.Context, recordingID string) ([]releaseGroup, error) {
	var groups []releaseGroup
	seen := map[string]bool{}
	for offset := 0; ; {
		path := "/ws/2/release?recording=" + url.QueryEscape(recordingID) + "&inc=release-groups+artist-credits&status=official&fmt=json&limit=" +
			strconv.Itoa(musicBrainzPageSize) + "&offset=" + strconv.Itoa(offset)
		var page releasePageBody
		if err := m.getJSON(ctx, path, &page); err != nil {
			return nil, err
		}
		for _, release := range page.Releases {
			if group := release.ReleaseGroup; !seen[group.ID] {
				seen[group.ID] = true
				groups = append(groups, toReleaseGroup(group, release.ArtistCredit))
			}
		}
		offset += len(page.Releases)
		if len(page.Releases) == 0 || offset >= page.ReleaseCount {
			return groups, nil
		}
	}
}

func toReleaseGroup(body releaseGroupBody, releaseCredit artistCredit) releaseGroup {
	return releaseGroup{
		ID: body.ID, Title: body.Title, Artist: cmp.Or(body.ArtistCredit.text(), releaseCredit.text()), PrimaryType: body.PrimaryType,
		SecondaryTypes: body.SecondaryTypes, FirstReleaseDate: body.FirstReleaseDate,
	}
}

func toRecording(body recordingBody) recording {
	return recording{ID: body.ID, Title: body.Title, Artist: body.ArtistCredit.text(), Primary: body.ArtistCredit.primary(), ISRCs: body.ISRCs}
}

func (m musicBrainz) getJSON(ctx context.Context, path string, into any) error {
	raw, err := m.client.get(ctx, path)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, into); err != nil {
		return fmt.Errorf("decode musicbrainz response: %w", err)
	}
	return nil
}
