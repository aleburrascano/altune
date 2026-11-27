package domain

import (
	"altune/go-api/internal/shared/ids"
	"encoding/json"
	"slices"
	"time"
)

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func (t *Track) ID() TrackId                       { return t.id }
func (t *Track) UserId() ids.UserId                { return t.userId }
func (t *Track) Title() string                     { return t.title }
func (t *Track) Artist() string                    { return t.artist }
func (t *Track) Album() string                     { return t.album }
func (t *Track) DurationSeconds() *float64         { return clonePtr(t.durationSeconds) }
func (t *Track) AddedAt() time.Time                { return t.addedAt }
func (t *Track) ArtworkURL() *string               { return clonePtr(t.artworkURL) }
func (t *Track) DownloadStatus() DownloadStatus    { return t.downloadStatus }
func (t *Track) DedupKey() string                  { return t.dedupKey }
func (t *Track) IdempotencyKey() *string           { return clonePtr(t.idempotencyKey) }
func (t *Track) Year() *int                        { return clonePtr(t.year) }
func (t *Track) Genre() *string                    { return clonePtr(t.genre) }
func (t *Track) TrackNumber() *int                 { return clonePtr(t.trackNumber) }
func (t *Track) AlbumArtist() *string              { return clonePtr(t.albumArtist) }
func (t *Track) ISRC() *string                     { return clonePtr(t.isrc) }
func (t *Track) AudioRef() *string                 { return clonePtr(t.audioRef) }
func (t *Track) AudioVersion() string              { return t.audioVersion }
func (t *Track) FailureReason() *string            { return clonePtr(t.failureReason) }
func (t *Track) FeaturedArtists() []FeaturedArtist { return slices.Clone(t.featuredArtists) }

func (t *Track) AcquisitionProvenance() *string { return clonePtr(t.acquisitionProvenance) }
func (t *Track) AudioSourceURL() *string        { return clonePtr(t.audioSourceURL) }

func (t *Track) ConfidenceScore() *float64     { return clonePtr(t.confidenceScore) }
func (t *Track) EvidenceJSON() json.RawMessage { return slices.Clone(t.evidenceJSON) }

func (t *Track) CatalogDurationSeconds() *float64 { return clonePtr(t.catalogDurationSeconds) }
func (t *Track) CatalogMBID() *string             { return clonePtr(t.catalogMBID) }
func (t *Track) CatalogExplicit() *bool           { return clonePtr(t.catalogExplicit) }
func (t *Track) CatalogSources() []CatalogSource  { return slices.Clone(t.catalogSources) }
func (t *Track) AudioLengthMismatch() bool        { return t.audioLengthMismatch }

func (t *Track) AcquisitionStartedAt() *time.Time { return clonePtr(t.acquisitionStartedAt) }

func (t *Track) Version() int          { return t.version }
func (t *Track) AcquisitionFence() int { return t.acquisitionFence }

func (t *Track) RecordVersion(version int) { t.version = version }
