package memory

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"slices"
	"testing"
	"time"
)

type UnreleasedStoreUnderTest interface {
	ports.UnreleasedStore
	NewRecordedArtist(t *testing.T) ports.UnreleasedArtistKey
	NewUnrecordedArtist(t *testing.T) ports.UnreleasedArtistKey
}

func UnreleasedStoreContract(t *testing.T, newStore func(t *testing.T) UnreleasedStoreUnderTest) {
	t.Helper()
	contracts := map[string]func(*testing.T, UnreleasedStoreUnderTest){
		"an artist that was recorded but never built reads as not built":         contractRecordedArtistIsNotBuilt,
		"a write is read back with its songs in order, its flag and its version": contractWriteRoundTrips,
		"a write started before the stored build is ignored":                     contractStaleWriteIsIgnored,
		"a write started after the stored build replaces it":                     contractNewerWriteReplaces,
		"a write for an artist that was never recorded stores nothing":           contractUnrecordedWriteStoresNothing,
		"recording an artist again leaves a built artist built":                  contractRecordAgainKeepsTheBuild,
	}
	for name, contract := range contracts {
		t.Run(name, func(t *testing.T) { contract(t, newStore(t)) })
	}
}

func titlesOf(songs []domain.UnreleasedSong) []string {
	out := make([]string, len(songs))
	for i, s := range songs {
		out[i] = s.Title
	}
	return out
}

func readOrFail(t *testing.T, s UnreleasedStoreUnderTest, key ports.UnreleasedArtistKey) (songs []domain.UnreleasedSong, complete, built bool, version int) {
	t.Helper()
	songs, complete, built, version, err := s.Read(context.Background(), key)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	return songs, complete, built, version
}

func writeOrFail(t *testing.T, s UnreleasedStoreUnderTest, key ports.UnreleasedArtistKey, titles []string, complete bool, startedAt time.Time, version int) {
	t.Helper()
	songs := make([]domain.UnreleasedSong, len(titles))
	for i, title := range titles {
		songs[i] = domain.UnreleasedSong{Title: title}
	}
	if err := s.Write(context.Background(), key, songs, complete, startedAt, version); err != nil {
		t.Fatalf("Write: %v", err)
	}
}

func contractBuildStart() time.Time {
	return time.Now().Add(-time.Hour).UTC().Truncate(time.Microsecond)
}

func contractRecordedArtistIsNotBuilt(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewRecordedArtist(t)

	songs, complete, built, version := readOrFail(t, s, key)

	if built || complete || len(songs) != 0 || version != 0 {
		t.Errorf("Read = (%v, complete %v, built %v, version %d), want nothing built", titlesOf(songs), complete, built, version)
	}
}

func contractWriteRoundTrips(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewRecordedArtist(t)
	writeOrFail(t, s, key, []string{"A", "B", "C"}, true, contractBuildStart(), 3)

	songs, complete, built, version := readOrFail(t, s, key)

	if got := titlesOf(songs); !slices.Equal(got, []string{"A", "B", "C"}) || !complete || !built || version != 3 {
		t.Errorf("Read = (%v, complete %v, built %v, version %d), want [A B C] complete, built, version 3", got, complete, built, version)
	}
}

func contractStaleWriteIsIgnored(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewRecordedArtist(t)
	started := contractBuildStart()
	writeOrFail(t, s, key, []string{"A", "B", "C"}, true, started, 1)

	writeOrFail(t, s, key, []string{"Stale"}, false, started.Add(-time.Minute), 1)

	songs, complete, _, _ := readOrFail(t, s, key)
	if got := titlesOf(songs); !slices.Equal(got, []string{"A", "B", "C"}) || !complete {
		t.Errorf("after a stale write Read = (%v, complete %v), want the first build untouched", got, complete)
	}
}

func contractNewerWriteReplaces(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewRecordedArtist(t)
	started := contractBuildStart()
	writeOrFail(t, s, key, []string{"A", "B", "C"}, true, started, 1)

	writeOrFail(t, s, key, []string{"D"}, false, started.Add(time.Minute), 2)

	songs, complete, _, version := readOrFail(t, s, key)
	if got := titlesOf(songs); !slices.Equal(got, []string{"D"}) || complete || version != 2 {
		t.Errorf("after a newer write Read = (%v, complete %v, version %d), want [D] incomplete at version 2", got, complete, version)
	}
}

func contractUnrecordedWriteStoresNothing(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewUnrecordedArtist(t)

	writeOrFail(t, s, key, []string{"A"}, true, contractBuildStart(), 1)

	songs, _, built, _ := readOrFail(t, s, key)
	if built || len(songs) != 0 {
		t.Errorf("Read = (%v, built %v), want nothing stored for an artist nobody recorded", titlesOf(songs), built)
	}
}

func contractRecordAgainKeepsTheBuild(t *testing.T, s UnreleasedStoreUnderTest) {
	key := s.NewRecordedArtist(t)
	writeOrFail(t, s, key, []string{"A"}, true, contractBuildStart(), 1)

	if err := s.Record(context.Background(), key, "Renamed", map[domain.ProviderName]string{domain.ProviderMusicBrainz: "mb2"}); err != nil {
		t.Fatalf("Record again: %v", err)
	}

	songs, _, built, _ := readOrFail(t, s, key)
	if !built || !slices.Equal(titlesOf(songs), []string{"A"}) {
		t.Errorf("Read = (%v, built %v), want the build kept after a second Record", titlesOf(songs), built)
	}
}
