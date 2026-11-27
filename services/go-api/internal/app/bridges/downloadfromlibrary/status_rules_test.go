package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/library"
	"testing"
)

type fakeLedgerOnLibraryRules struct{ *downloadtest.Ledger }

func (l fakeLedgerOnLibraryRules) AddTrack(_ *testing.T, snapshot download.TrackSnapshot) {
	l.Seed(downloadtest.LedgerTrack{TrackSnapshot: snapshot})
}

func TestFakeLedger_OnTheLibrarysRealRulesMeetsTheLedgerContract(t *testing.T) {
	downloadtest.LedgerContract(t, func(*testing.T) downloadtest.LedgerUnderTest {
		return fakeLedgerOnLibraryRules{downloadtest.NewLedger().UsingRules(downloadfromlibrary.Rules{})}
	})
}

func TestRules_AnswerLikeTheLibraryForEveryStatusAndEvent(t *testing.T) {
	t.Parallel()

	events := map[download.TrackEvent]library.AcquisitionEvent{
		download.EventMarkReady:       library.EventMarkReady,
		download.EventReplaceAudio:    library.EventReplaceAudio,
		download.EventMarkFailed:      library.EventMarkFailed,
		download.EventFailPending:     library.EventFailPending,
		download.EventRevertToPending: library.EventRevertToPending,
	}
	statuses := map[download.TrackStatus]library.DownloadStatus{
		download.TrackPending: library.AcquisitionPending,
		download.TrackReady:   library.AcquisitionReady,
		download.TrackFailed:  library.AcquisitionFailed,
	}
	for event, libraryEvent := range events {
		for from, libraryFrom := range statuses {
			_, wantErr := library.Transition(libraryFrom, libraryEvent)

			_, err := downloadfromlibrary.Rules{}.Next(from, event)

			if (err == nil) != (wantErr == nil) {
				t.Errorf("event %d from %d: Rules err = %v, library err = %v", event, from, err, wantErr)
			}
		}
	}
}
