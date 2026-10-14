package main

import (
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"unicode"
)

const (
	maxAlbumCandidates  = 5
	maxTracksPerAlbum   = 3
	masterFitSeconds    = 10.0
	catalogLengthSlack  = 1.0
	albumTitleSeparator = " - "
)

type deezerTrack struct {
	ID              string
	ISRC            string
	Title           string
	Position        int
	DurationSeconds float64
	AlbumID         string
	AlbumTitle      string
}

type deezerAlbumTrack struct {
	ID    string
	Title string
}

type deezerAlbum struct {
	ID       string
	Title    string
	CoverURL string
	Genre    string
	Tracks   []deezerAlbumTrack
}

type deezerCatalog interface {
	TrackByISRC(ctx context.Context, isrc string) (deezerTrack, error)
	TrackByID(ctx context.Context, id string) (deezerTrack, error)
	SearchAlbums(ctx context.Context, artist, title string) ([]deezerAlbum, error)
	Album(ctx context.Context, id string) (deezerAlbum, error)
}

type outcome string

const (
	outcomeSameISRC    outcome = "same_isrc"
	outcomeOtherMaster outcome = "other_master"
	outcomeNoAlbum     outcome = "no_album"
)

type deezerMatch struct {
	AlbumID         string
	AlbumTitle      string
	CoverURL        string
	Genre           string
	TrackPosition   int
	DurationSeconds float64
	ISRC            string
}

type crossCheck struct {
	Outcome     outcome
	Match       *deezerMatch
	OtherMaster *deezerTrack
}

func canonicalISRC(isrc string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(isrc), "-", ""))
}

func normalizeName(s string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			out.WriteRune(r)
		}
	}
	return out.String()
}

func baseTitle(s string) string {
	for _, open := range []string{" (", " [", albumTitleSeparator} {
		if i := strings.Index(s, open); i > 0 {
			s = s[:i]
		}
	}
	return s
}

func sameTitle(a, b string) bool {
	na, nb := normalizeName(baseTitle(a)), normalizeName(baseTitle(b))
	return na != "" && na == nb
}

type crossChecker struct{ deezer deezerCatalog }

type crossCheckInput struct {
	ISRC           string
	Artist         string
	SongTitle      string
	Group          releaseGroup
	RecordingISRCs []string
}

func (c crossChecker) check(ctx context.Context, in crossCheckInput) (crossCheck, error) {
	byISRC, err := c.deezer.TrackByISRC(ctx, in.ISRC)
	if err != nil && !errors.Is(err, errCatalogUnknown) {
		return crossCheck{}, err
	}
	if err == nil && sameTitle(byISRC.AlbumTitle, in.Group.Title) {
		return c.matchOf(ctx, byISRC)
	}
	return c.searchArtistAlbums(ctx, in)
}

func (c crossChecker) matchOf(ctx context.Context, track deezerTrack) (crossCheck, error) {
	album, err := c.deezer.Album(ctx, track.AlbumID)
	if err != nil {
		return crossCheck{}, err
	}
	return crossCheck{Outcome: outcomeSameISRC, Match: &deezerMatch{
		AlbumID: album.ID, AlbumTitle: album.Title, CoverURL: album.CoverURL, Genre: album.Genre,
		TrackPosition: track.Position, DurationSeconds: track.DurationSeconds, ISRC: track.ISRC,
	}}, nil
}

func (c crossChecker) searchArtistAlbums(ctx context.Context, in crossCheckInput) (crossCheck, error) {
	found, err := c.deezer.SearchAlbums(ctx, in.Artist, in.Group.Title)
	if errors.Is(err, errCatalogUnknown) {
		return crossCheck{Outcome: outcomeNoAlbum}, nil
	}
	if err != nil {
		return crossCheck{}, err
	}
	same, otherMaster, err := c.scanAlbums(ctx, firstSameTitled(found, in.Group.Title, maxAlbumCandidates), in)
	switch {
	case err != nil:
		return crossCheck{}, err
	case same != nil:
		return c.matchOf(ctx, *same)
	case otherMaster != nil:
		return crossCheck{Outcome: outcomeOtherMaster, OtherMaster: otherMaster}, nil
	}
	return crossCheck{Outcome: outcomeNoAlbum}, nil
}

func (c crossChecker) scanAlbums(ctx context.Context, candidates []deezerAlbum, in crossCheckInput) (*deezerTrack, *deezerTrack, error) {
	var otherMaster *deezerTrack
	for _, candidate := range candidates {
		album, err := c.deezer.Album(ctx, candidate.ID)
		if errors.Is(err, errCatalogUnknown) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		same, master, err := c.findRecordingTrack(ctx, album, in)
		if err != nil || same != nil {
			return same, nil, err
		}
		if otherMaster == nil {
			otherMaster = master
		}
	}
	return nil, otherMaster, nil
}

func firstSameTitled(albums []deezerAlbum, title string, limit int) []deezerAlbum {
	var same []deezerAlbum
	for _, album := range albums {
		if sameTitle(album.Title, title) && len(same) < limit {
			same = append(same, album)
		}
	}
	return same
}

func songEntries(album deezerAlbum, songTitle string) []deezerAlbumTrack {
	var entries []deezerAlbumTrack
	for _, entry := range album.Tracks {
		if sameTitle(entry.Title, songTitle) && len(entries) < maxTracksPerAlbum {
			entries = append(entries, entry)
		}
	}
	return entries
}

func (c crossChecker) findRecordingTrack(ctx context.Context, album deezerAlbum, in crossCheckInput) (*deezerTrack, *deezerTrack, error) {
	var otherMaster *deezerTrack
	for _, entry := range songEntries(album, in.SongTitle) {
		track, err := c.deezer.TrackByID(ctx, entry.ID)
		if errors.Is(err, errCatalogUnknown) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		track.AlbumID, track.AlbumTitle = album.ID, album.Title
		switch {
		case canonicalISRC(track.ISRC) == canonicalISRC(in.ISRC):
			return &track, nil, nil
		case otherMaster == nil && listsISRC(in.RecordingISRCs, track.ISRC):
			otherMaster = &track
		}
	}
	return nil, otherMaster, nil
}

func listsISRC(isrcs []string, isrc string) bool {
	return isrc != "" && slices.ContainsFunc(isrcs, func(listed string) bool { return canonicalISRC(listed) == canonicalISRC(isrc) })
}

func fitsFile(fileSeconds, catalogSeconds float64) bool {
	return fileSeconds > 0 && catalogSeconds > 0 && math.Abs(fileSeconds-catalogSeconds) <= masterFitSeconds
}
