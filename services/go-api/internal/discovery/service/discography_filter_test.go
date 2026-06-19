package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func album(title string, year int) domain.SearchResult {
	extras := map[string]any{}
	if year > 0 {
		extras["year"] = year
	}
	return domain.SearchResult{
		Kind:   domain.ResultKindAlbum,
		Title:  title,
		Extras: extras,
	}
}

func TestFilterContamination(t *testing.T) {
	confirmed := map[string]bool{
		NormalizeForMatch("REST IN BASS"):         true,
		NormalizeForMatch("Sayso Says"):            true,
		NormalizeForMatch("closed captions"):       true,
		NormalizeForMatch("REST IN BASS: ENCORE"):  true,
	}

	t.Run("keeps confirmed albums", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("REST IN BASS", 2022),
			album("Sayso Says", 2021),
			album("closed captions", 2023),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: confirmed,
		})
		if len(got) != 3 {
			t.Errorf("expected 3 kept, got %d", len(got))
		}
	})

	t.Run("removes album with year mismatch + unconfirmed", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("REST IN BASS", 2022),
			album("Samsonite", 1995),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: confirmed,
		})
		if len(got) != 1 {
			t.Fatalf("expected 1 kept, got %d", len(got))
		}
		if got[0].Title != "REST IN BASS" {
			t.Errorf("expected REST IN BASS, got %s", got[0].Title)
		}
	})

	t.Run("keeps unconfirmed album with valid year (single mismatch)", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("LOTTO DREAMS", 2024),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: confirmed,
		})
		if len(got) != 1 {
			t.Errorf("expected 1 kept (single mismatch insufficient), got %d", len(got))
		}
	})

	t.Run("keeps REST IN BASS ENCORE even though not in MB", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("REST IN BASS: ENCORE", 2025),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: confirmed,
		})
		if len(got) != 1 {
			t.Errorf("expected ENCORE kept, got %d", len(got))
		}
	})

	t.Run("no filtering when birth year unknown", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("Samsonite", 1995),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 0, MBConfirmed: confirmed,
		})
		if len(got) != 1 {
			t.Errorf("expected 1 kept (no birth year), got %d", len(got))
		}
	})

	t.Run("no filtering when confirmed set empty", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("Samsonite", 1995),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: map[string]bool{},
		})
		if len(got) != 1 {
			t.Errorf("expected 1 kept (no confirmed set), got %d", len(got))
		}
	})

	t.Run("removes multiple contaminated albums", func(t *testing.T) {
		albums := []domain.SearchResult{
			album("REST IN BASS", 2022),
			album("Gallos Ciegos", 2003),
			album("Tšernobõl", 2001),
			album("Kiss Me in the Sky", 1998),
			album("Sayso Says", 2021),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 2006, MBConfirmed: confirmed,
		})
		if len(got) != 2 {
			t.Errorf("expected 2 kept (REST IN BASS + Sayso Says), got %d", len(got))
			for _, a := range got {
				t.Logf("  kept: %s", a.Title)
			}
		}
	})

	t.Run("mainstream artist no filtering", func(t *testing.T) {
		drakeConfirmed := map[string]bool{
			NormalizeForMatch("Scorpion"):       true,
			NormalizeForMatch("Certified Lover Boy"): true,
			NormalizeForMatch("Views"):          true,
		}
		albums := []domain.SearchResult{
			album("Scorpion", 2018),
			album("Certified Lover Boy", 2021),
			album("Views", 2016),
		}
		got := FilterContamination(albums, DiscographyFilterInput{
			BirthYear: 1986, MBConfirmed: drakeConfirmed,
		})
		if len(got) != 3 {
			t.Errorf("expected all 3 Drake albums kept, got %d", len(got))
		}
	})
}
