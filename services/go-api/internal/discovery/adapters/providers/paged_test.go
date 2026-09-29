package providers

import (
	"altune/go-api/internal/discovery/domain"
	"errors"
	"testing"
)

func TestFetchPaged_laterPageFailureReturnsPartialResultError(t *testing.T) {
	boom := errors.New("boom")
	var loggedPage int
	got, err := fetchPaged(3,
		func(page int) ([]int, bool, error) {
			if page == 2 {
				return nil, false, boom
			}
			return []int{page}, true, nil
		},
		func(page int, _ error) { loggedPage = page })

	var partial *domain.PartialResultError
	if !errors.As(err, &partial) || partial.Page != 2 || !errors.Is(err, boom) {
		t.Fatalf("err = %v, want a PartialResultError for page 2 wrapping the cause", err)
	}
	if len(got) != 2 || got[0] != 0 || got[1] != 1 {
		t.Errorf("got = %v, want the pages fetched before the failure", got)
	}
	if loggedPage != 2 {
		t.Errorf("onPartial page = %d, want 2", loggedPage)
	}
}

func TestFetchPaged_firstPageFailureIsAHardError(t *testing.T) {
	boom := errors.New("boom")
	got, err := fetchPaged(3,
		func(int) ([]int, bool, error) { return nil, false, boom },
		func(int, error) { t.Error("onPartial called for a page-0 failure") })
	var partial *domain.PartialResultError
	if !errors.Is(err, boom) || errors.As(err, &partial) || got != nil {
		t.Errorf("got = %v, err = %v, want nil and the plain page-0 error", got, err)
	}
}

func TestFetchPaged_completeFetchHasNoError(t *testing.T) {
	got, err := fetchPaged(3,
		func(page int) ([]int, bool, error) { return []int{page}, page < 1, nil },
		func(int, error) {})
	if err != nil || len(got) != 2 {
		t.Errorf("got = %v, err = %v, want 2 items and nil", got, err)
	}
}
