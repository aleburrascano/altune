package memory_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"context"
	"strconv"
	"sync/atomic"
	"testing"
)

type recordedArtists struct {
	*memory.UnreleasedStore
	next atomic.Int64
}

func (r *recordedArtists) NewUnrecordedArtist(*testing.T) ports.UnreleasedArtistKey {
	return ports.UnreleasedArtistKey{Provider: domain.ProviderDeezer, ExternalID: "fake-" + strconv.FormatInt(r.next.Add(1), 10)}
}

func (r *recordedArtists) NewRecordedArtist(t *testing.T) ports.UnreleasedArtistKey {
	key := r.NewUnrecordedArtist(t)
	if err := r.Record(context.Background(), key, "Artist "+key.ExternalID, map[domain.ProviderName]string{domain.ProviderMusicBrainz: "mb"}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	return key
}

func TestUnreleasedStore_KeepsTheStoreContract(t *testing.T) {
	memory.UnreleasedStoreContract(t, func(*testing.T) memory.UnreleasedStoreUnderTest {
		return &recordedArtists{UnreleasedStore: &memory.UnreleasedStore{}}
	})
}
