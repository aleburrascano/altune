package catalogbridge

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

const seededAudioRef = "eval/stored.mp3"

type SeededFile struct {
	Duration float64
}

type SeededTrack struct {
	Title              string
	Artist             string
	Album              string
	ISRC               string
	CatalogDuration    float64
	CatalogMBID        string
	CatalogExplicit    *bool
	CatalogSources     []ports.CatalogSource
	AudioSourceURL     string
	RejectedSourceKeys []string
	Stored             *SeededFile
}

type SavedTrack struct {
	Title              string
	Album              string
	Year               int
	CatalogMBID        string
	ISRC               string
	Provenance         string
	DurationSeconds    float64
	RejectedSourceKeys []string
	Evidence           json.RawMessage
}

type SeededLedger struct {
	*TrackLedger
	repo    *catalogtest.TrackRepo
	userID  shared.UserId
	trackID shared.TrackId
}

func NewSeededLedger(seed SeededTrack) (*SeededLedger, error) {
	userID := shared.NewUserId(uuid.New())
	track, err := seededDomainTrack(userID, seed)
	if err != nil {
		return nil, err
	}
	repo := catalogtest.NewTrackRepo()
	repo.Seed(track)
	return &SeededLedger{TrackLedger: NewTrackLedger(repo), repo: repo, userID: userID, trackID: track.ID}, nil
}

func seededDomainTrack(userID shared.UserId, seed SeededTrack) (*domain.Track, error) {
	track, err := domain.NewTrack(userID, seed.Title, seed.Artist, seed.Album)
	if err != nil {
		return nil, fmt.Errorf("new track: %w", err)
	}
	track.Album = seed.Album
	if seed.ISRC != "" {
		track.ISRC = &seed.ISRC
	}
	if err := track.SetCatalogDuration(seed.CatalogDuration); err != nil {
		return nil, fmt.Errorf("catalog duration: %w", err)
	}
	seedCatalogPins(track, seed)
	track.SetAudioSource(seed.AudioSourceURL)
	for _, key := range seed.RejectedSourceKeys {
		track.RejectAudioSource(key)
	}
	return track, seedStoredFile(track, seed.Stored)
}

func seedCatalogPins(track *domain.Track, seed SeededTrack) {
	if seed.CatalogMBID != "" {
		track.CatalogMBID = &seed.CatalogMBID
	}
	track.CatalogExplicit = seed.CatalogExplicit
	for _, s := range seed.CatalogSources {
		track.CatalogSources = append(track.CatalogSources, domain.CatalogSource(s))
	}
}

func seedStoredFile(track *domain.Track, stored *SeededFile) error {
	if stored == nil {
		return nil
	}
	if err := track.MarkReady(seededAudioRef); err != nil {
		return fmt.Errorf("stored file: %w", err)
	}
	if err := track.SetDuration(stored.Duration); err != nil {
		return fmt.Errorf("stored duration: %w", err)
	}
	return nil
}

func (l *SeededLedger) UserID() shared.UserId { return l.userID }

func (l *SeededLedger) TrackID() shared.TrackId { return l.trackID }

func (l *SeededLedger) Saved() SavedTrack {
	track, err := l.repo.GetByID(context.Background(), l.trackID, l.userID)
	if err != nil || track == nil {
		return SavedTrack{}
	}
	return SavedTrack{
		Title:              track.Title,
		Album:              track.Album,
		Year:               derefOrZero(track.Year),
		CatalogMBID:        derefOrZero(track.CatalogMBID),
		ISRC:               derefOrZero(track.ISRC),
		Provenance:         derefOrZero(track.AcquisitionProvenance),
		DurationSeconds:    derefOrZero(track.DurationSeconds),
		RejectedSourceKeys: track.RejectedSourceKeys,
		Evidence:           track.EvidenceJSON,
	}
}

func derefOrZero[T any](p *T) T {
	var zero T
	if p == nil {
		return zero
	}
	return *p
}
