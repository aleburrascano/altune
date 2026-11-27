package downloadfromlibrary

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"fmt"
)

var _ download.StatusRules = Rules{}

type Rules struct{}

func (Rules) Next(from download.TrackStatus, event download.TrackEvent) (download.TrackStatus, error) {
	libraryFrom, err := libraryStatus(from)
	if err != nil {
		return from, err
	}
	libraryEvent, err := libraryEventOf(event)
	if err != nil {
		return from, err
	}
	next, err := library.Transition(libraryFrom, libraryEvent)
	if err != nil {
		return from, translate(err)
	}
	return mapStatus(next)
}

func (Rules) Claimable() []download.TrackStatus { return mapStatuses(library.ClaimableStatuses()) }

func (Rules) Retryable() []download.TrackStatus { return mapStatuses(library.RetryableStatuses()) }

func (Rules) Upgradable() []download.TrackStatus { return mapStatuses(library.UpgradableStatuses()) }

func (l *LibraryTracks) Next(from download.TrackStatus, event download.TrackEvent) (download.TrackStatus, error) {
	return Rules{}.Next(from, event)
}

func (l *LibraryTracks) Claimable() []download.TrackStatus { return Rules{}.Claimable() }

func (l *LibraryTracks) Retryable() []download.TrackStatus { return Rules{}.Retryable() }

func (l *LibraryTracks) Upgradable() []download.TrackStatus { return Rules{}.Upgradable() }

func mapStatuses(statuses []library.DownloadStatus) []download.TrackStatus {
	mapped := make([]download.TrackStatus, 0, len(statuses))
	for _, s := range statuses {
		m, err := mapStatus(s)
		if err != nil {
			panic(err)
		}
		mapped = append(mapped, m)
	}
	return mapped
}

func libraryStatus(s download.TrackStatus) (library.DownloadStatus, error) {
	for _, candidate := range library.AcquisitionStatuses() {
		if mapped, err := mapStatus(candidate); err == nil && mapped == s {
			return candidate, nil
		}
	}
	return 0, fmt.Errorf("%w: %d", errUnmappedStatus, int(s))
}

func libraryEventOf(e download.TrackEvent) (library.AcquisitionEvent, error) {
	switch e {
	case download.EventMarkReady:
		return library.EventMarkReady, nil
	case download.EventReplaceAudio:
		return library.EventReplaceAudio, nil
	case download.EventMarkFailed:
		return library.EventMarkFailed, nil
	case download.EventFailPending:
		return library.EventFailPending, nil
	case download.EventRevertToPending:
		return library.EventRevertToPending, nil
	}
	return 0, fmt.Errorf("unknown acquisition event %d", int(e))
}
