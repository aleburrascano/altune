package main

import (
	"altune/go-api/internal/shared/songfacts"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
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

func (r recording) leadArtist() string {
	return cmp.Or(r.Primary, r.Artist)
}

func (r recording) hasISRC(isrc string) bool {
	return slices.ContainsFunc(r.ISRCs, func(listed string) bool { return songfacts.CanonicalISRC(listed) == songfacts.CanonicalISRC(isrc) })
}

type recordingSource interface {
	RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error)
	Recording(ctx context.Context, id string) (recording, error)
	ReleaseGroups(ctx context.Context, recordingID string) ([]releaseGroup, error)
	ReleaseTrackSeconds(ctx context.Context, releaseID, recordingID string) (float64, error)
}

type musicBrainz struct{ client *catalogClient }

func newMusicBrainz(httpClient *http.Client, baseURL string, requestsPerSecond float64) musicBrainz {
	return musicBrainz{client: newCatalogClient(httpClient, baseURL, musicBrainzUserAgent, requestsPerSecond, isMusicBrainzThrottle)}
}

func isMusicBrainzThrottle(status int, _ []byte) bool {
	return status == http.StatusServiceUnavailable || status == http.StatusTooManyRequests
}

var featureJoinRe = regexp.MustCompile(`(?i)\b(?:feat|ft|featuring|with)\b`)

type artistCredit []struct {
	Name       string `json:"name"`
	JoinPhrase string `json:"joinphrase"`
	Artist     struct {
		Name string `json:"name"`
	} `json:"artist"`
}

func (c artistCredit) primary() string {
	if len(c) == 0 {
		return ""
	}
	return cmp.Or(c[0].Artist.Name, c[0].Name)
}

func (c artistCredit) text() string {
	var out strings.Builder
	for i, credit := range c {
		out.WriteString(cmp.Or(credit.Artist.Name, credit.Name))
		if i == len(c)-1 || featureJoinRe.MatchString(credit.JoinPhrase) {
			break
		}
		out.WriteString(credit.JoinPhrase)
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
		ID           string           `json:"id"`
		ArtistCredit artistCredit     `json:"artist-credit"`
		ReleaseGroup releaseGroupBody `json:"release-group"`
		CoverArt     struct {
			Front bool `json:"front"`
		} `json:"cover-art-archive"`
	} `json:"releases"`
}

type releaseTracksBody struct {
	Media []struct {
		Tracks []struct {
			LengthMillis int `json:"length"`
			Recording    struct {
				ID string `json:"id"`
			} `json:"recording"`
		} `json:"tracks"`
	} `json:"media"`
}

func (m musicBrainz) RecordingsByISRC(ctx context.Context, isrc string) ([]recording, error) {
	query := `isrc:"` + songfacts.CanonicalISRC(isrc) + `"`
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
	groups := []releaseGroup{}
	indexOf := map[string]int{}
	for offset := 0; ; {
		path := "/ws/2/release?recording=" + url.QueryEscape(recordingID) + "&inc=release-groups+artist-credits&status=official&fmt=json&limit=" +
			strconv.Itoa(musicBrainzPageSize) + "&offset=" + strconv.Itoa(offset)
		var page releasePageBody
		if err := m.getJSON(ctx, path, &page); err != nil {
			return nil, err
		}
		for _, release := range page.Releases {
			index, known := indexOf[release.ReleaseGroup.ID]
			if !known {
				index = len(groups)
				indexOf[release.ReleaseGroup.ID] = index
				groups = append(groups, toReleaseGroup(release.ReleaseGroup, release.ArtistCredit))
				groups[index].ReleaseID = release.ID
			}
			if release.CoverArt.Front && groups[index].FrontReleaseID == "" {
				groups[index].FrontReleaseID = release.ID
			}
		}
		offset += len(page.Releases)
		if len(page.Releases) == 0 || offset >= page.ReleaseCount {
			return groups, nil
		}
	}
}

func (m musicBrainz) ReleaseTrackSeconds(ctx context.Context, releaseID, recordingID string) (float64, error) {
	var body releaseTracksBody
	if err := m.getJSON(ctx, "/ws/2/release/"+url.PathEscape(releaseID)+"?inc=recordings&fmt=json", &body); err != nil {
		return 0, err
	}
	for _, medium := range body.Media {
		for _, track := range medium.Tracks {
			if track.Recording.ID == recordingID && track.LengthMillis > 0 {
				return math.Round(float64(track.LengthMillis) / millisPerSecond), nil
			}
		}
	}
	return 0, errCatalogUnknown
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
