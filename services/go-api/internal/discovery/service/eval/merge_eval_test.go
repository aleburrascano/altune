package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestRunMergeEval_clean(t *testing.T) {
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {track("Humble", "Kendrick", domain.ProviderDeezer, nil)},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 0 || r.UnderMergeRate() != 0 {
		t.Fatalf("expected zero under-merge, got incidents=%d rate=%v", r.UnderMergeIncidents, r.UnderMergeRate())
	}
	if r.Evaluated != 1 || r.CleanQueries != 1 {
		t.Errorf("expected 1 clean evaluated query, got evaluated=%d clean=%d", r.Evaluated, r.CleanQueries)
	}
}

func TestRunMergeEval_underMergeIdenticalCanonical(t *testing.T) {
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {
			track("Humble", "Kendrick", domain.ProviderDeezer, nil),
			track("Humble", "Kendrick", domain.ProviderITunes, nil),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 1 {
		t.Fatalf("expected 1 under-merge incident, got %d", r.UnderMergeIncidents)
	}
	if r.ResultsSeen != 2 || r.UnderMergeRate() != 0.5 {
		t.Errorf("under_merge_rate = %v (incidents %d / seen %d), want 0.5", r.UnderMergeRate(), r.UnderMergeIncidents, r.ResultsSeen)
	}
	if len(r.Failures()) != 1 || r.Failures()[0].Reason != "under_merge" {
		t.Errorf("expected one under_merge failure, got %+v", r.Failures())
	}
}

func TestRunMergeEval_distinctVersionsNotUnderMerge(t *testing.T) {
	entities := []LibraryEntity{{Title: "Redrum", Artist: "21 Savage"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"21 Savage Redrum": {
			track("Redrum", "21 Savage", domain.ProviderSoundCloud, nil),
			track("Redrum sped up", "21 Savage", domain.ProviderSoundCloud, nil),
			track("Redrum slowed", "21 Savage", domain.ProviderSoundCloud, nil),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 0 {
		t.Errorf("distinct versions must not count as under-merge, got %d incidents", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_underMergeByISRC(t *testing.T) {
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Kendrick Humble": {
			track("HUMBLE.", "Kendrick", domain.ProviderDeezer, map[string]any{"isrc": "USUM71703089"}),
			track("Humble (Clean)", "Kendrick", domain.ProviderITunes, map[string]any{"isrc": "USUM71703089"}),
		},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.UnderMergeIncidents != 1 {
		t.Errorf("shared ISRC across two rows must be an under-merge, got %d", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_overMerge(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "Love", Artist: "Artist"},
		{Title: "Story", Artist: "Artist"},
	}
	merged := track("Love Story", "Artist", domain.ProviderDeezer, nil)
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Artist Love":  {merged},
		"Artist Story": {merged},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.OverMerged != 1 || r.OverMergeRate() != 1.0 {
		t.Fatalf("expected over-merge rate 1.0, got over=%d rate=%v", r.OverMerged, r.OverMergeRate())
	}
}

func TestRunMergeEval_noMatchExcluded(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "Found", Artist: "Artist"},
		{Title: "Missing", Artist: "Artist"},
	}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Artist Found": {track("Found", "Artist", domain.ProviderDeezer, nil)},
	}}
	r := RunMergeEval(context.Background(), entities, fake, 1, nil)
	if r.NoMatch != 1 || r.Evaluated != 1 {
		t.Errorf("coverage miss handling wrong: no_match=%d evaluated=%d", r.NoMatch, r.Evaluated)
	}
}

func isrcTrack(title, isrc string, provider domain.ProviderName) domain.SearchResult {
	r := track(title, "Adele", provider, nil)
	r.ISRC = isrc
	return r
}

func TestRunMergeEval_sameISRCInTwoSpellingsIsOneUnderMerge(t *testing.T) {
	entities := []LibraryEntity{{Title: "Hello", Artist: "Adele"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Adele Hello": {
			isrcTrack("Hello", "GBBKS1500214", domain.ProviderDeezer),
			isrcTrack("Hello", "gb-bks-15-00214", domain.ProviderITunes),
		},
	}}

	r := RunMergeEval(context.Background(), entities, fake, 1, nil)

	if r.UnderMergeIncidents != 1 {
		t.Errorf("one ISRC spelled two ways is one recording left unmerged, got %d incidents", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_differentISRCsAreNeverAnUnderMerge(t *testing.T) {
	entities := []LibraryEntity{{Title: "Hello", Artist: "Adele"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Adele Hello": {
			isrcTrack("Hello", "GBBKS1500214", domain.ProviderDeezer),
			isrcTrack("Hello", "GBBKS1500215", domain.ProviderITunes),
		},
	}}

	r := RunMergeEval(context.Background(), entities, fake, 1, nil)

	if r.UnderMergeIncidents != 0 {
		t.Errorf("two recordings with different ISRCs are two songs, got %d incidents", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_studioAndLiveWithoutIdsAreNotUnderMerge(t *testing.T) {
	entities := []LibraryEntity{{Title: "Hello", Artist: "Adele"}}
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Adele Hello": {
			track("Hello", "Adele", domain.ProviderDeezer, nil),
			track("Hello (Live)", "Adele", domain.ProviderITunes, nil),
		},
	}}

	r := RunMergeEval(context.Background(), entities, fake, 1, nil)

	if r.UnderMergeIncidents != 0 {
		t.Errorf("a live version differs from the studio song by its version label, got %d incidents", r.UnderMergeIncidents)
	}
}

func TestRunMergeEval_overMergeSeparatesStudioFromLiveOwners(t *testing.T) {
	entities := []LibraryEntity{
		{Title: "Hello", Artist: "Adele"},
		{Title: "Hello (Live)", Artist: "Adele"},
	}
	studio := track("Hello", "Adele", domain.ProviderDeezer, nil)
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Adele Hello":        {studio},
		"Adele Hello (Live)": {track("Hello (Live)", "Adele", domain.ProviderITunes, nil)},
	}}

	r := RunMergeEval(context.Background(), entities, fake, 1, nil)

	if r.OverMerged != 0 {
		t.Errorf("studio and live are two owned songs on two cards, got %d over-merges", r.OverMerged)
	}
}

type failingMergeSearcher struct{}

func (failingMergeSearcher) Search(context.Context, string) ([]domain.SearchResult, error) {
	return nil, context.DeadlineExceeded
}

func TestRunMergeEval_erroredSearchesAreCountedNotSilentlyUnmatched(t *testing.T) {
	entities := []LibraryEntity{{Title: "Humble", Artist: "Kendrick"}}

	r := RunMergeEval(context.Background(), entities, failingMergeSearcher{}, 1, nil)

	if r.Errored != 1 || r.Evaluated != 0 || r.NoMatch != 0 {
		t.Errorf("errored=%d evaluated=%d no_match=%d, want 1/0/0", r.Errored, r.Evaluated, r.NoMatch)
	}
}

func hurtCorpus() []LibraryEntity {
	return []LibraryEntity{
		{Title: "Hurt", Artist: "Nine Inch Nails", Known: &KnownRecording{ISRCs: []string{"USIR10011001"}}},
		{Title: "Hurt", Artist: "Johnny Cash", Known: &KnownRecording{ISRCs: []string{"USAM10200002"}}},
	}
}

func hurtCard(artist, isrc string) domain.SearchResult {
	r := track("Hurt", artist, domain.ProviderDeezer, nil)
	r.ISRC = isrc
	r.Sources[0].ExternalID = "hurt-" + isrc
	return r
}

func TestRunMergeEval_knownISRCEntitiesMergedIntoOneCardAreAnOverMerge(t *testing.T) {
	merged := hurtCard("Johnny Cash", "USIR10011001")
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Nine Inch Nails Hurt": {merged},
		"Johnny Cash Hurt":     {merged},
	}}

	r := RunMergeEval(context.Background(), hurtCorpus(), fake, 1, nil)

	if r.OverMerged == 0 {
		t.Errorf("a card carrying NIN's ISRC under Cash's name must register as an over-merge, got %d", r.OverMerged)
	}
}

func TestRunMergeEval_knownISRCEntitiesKeptApartAreNotAnOverMerge(t *testing.T) {
	fake := &fakeSearcher{byQuery: map[string][]domain.SearchResult{
		"Nine Inch Nails Hurt": {hurtCard("Nine Inch Nails", "USIR10011001")},
		"Johnny Cash Hurt":     {hurtCard("Johnny Cash", "USAM10200002")},
	}}

	r := RunMergeEval(context.Background(), hurtCorpus(), fake, 1, nil)

	if r.OverMerged != 0 || r.Evaluated != 2 {
		t.Errorf("over=%d evaluated=%d, want 0/2", r.OverMerged, r.Evaluated)
	}
}
