package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
)

type fakeVerdicts struct {
	recorded bool
	err      error
	gotRef   string
	got      library.InPlaceVerdict
}

func (f *fakeVerdicts) RecordInPlaceVerdict(_ context.Context, _ ids.TrackId, audioRef string, v library.InPlaceVerdict) (bool, error) {
	f.gotRef, f.got = audioRef, v
	return f.recorded, f.err
}

func TestInPlaceVerdicts_HandsTheRecordToTheLibraryAndReturnsWhetherItWasRecorded(t *testing.T) {
	verdicts := &fakeVerdicts{recorded: true}

	recorded, err := downloadfromlibrary.NewInPlaceVerdicts(verdicts).RecordInPlaceVerdict(context.Background(), ids.NewTrackId(),
		download.InPlaceRecord{AudioRef: "ref", Verdict: "hard", Grade: "A", Basis: "acoustid", Score: 0.97, MayLower: true})

	if err != nil || !recorded {
		t.Fatalf("RecordInPlaceVerdict = %v, %v, want recorded", recorded, err)
	}
	if verdicts.gotRef != "ref" || verdicts.got.Verdict != "hard" || verdicts.got.Score != 0.97 {
		t.Errorf("library got ref %q and verdict %+v, want the record's fields", verdicts.gotRef, verdicts.got)
	}
}

func TestInPlaceVerdicts_AFileThatChangedMeanwhileIsReportedNotRecorded(t *testing.T) {
	recorded, err := downloadfromlibrary.NewInPlaceVerdicts(&fakeVerdicts{recorded: false}).
		RecordInPlaceVerdict(context.Background(), ids.NewTrackId(), download.InPlaceRecord{AudioRef: "old"})

	if err != nil || recorded {
		t.Errorf("RecordInPlaceVerdict = %v, %v, want not recorded and no error", recorded, err)
	}
}

func TestInPlaceVerdicts_ALibraryErrorIsReturned(t *testing.T) {
	verdicts := &fakeVerdicts{err: errors.New("library down")}

	_, err := downloadfromlibrary.NewInPlaceVerdicts(verdicts).RecordInPlaceVerdict(context.Background(), ids.NewTrackId(), download.InPlaceRecord{})

	if !errors.Is(err, verdicts.err) {
		t.Errorf("RecordInPlaceVerdict = %v, want the library error", err)
	}
}
