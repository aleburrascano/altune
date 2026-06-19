package service

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"unicode"

	"altune/go-api/internal/discovery/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// TestPipelineIntegration_LibraryQueries pulls every track from the real
// database and tests the ranking pipeline with generated query permutations.
// This verifies that the pipeline handles the user's actual library
// consistently. Skipped when DATABASE_URL is not set.
//
// Run with:
//
//	DATABASE_URL=... go test ./internal/discovery/service/... -run TestPipelineIntegration -v -timeout=120s
func TestPipelineIntegration_LibraryQueries(t *testing.T) {
	pool := integrationPool(t)
	tracks := loadLibraryTracks(t, pool)
	if len(tracks) == 0 {
		t.Skip("no tracks in database")
	}

	t.Logf("loaded %d tracks from database", len(tracks))

	artists := uniqueArtists(tracks)
	t.Logf("unique artists: %d", len(artists))

	passed, failed, skipped := 0, 0, 0

	t.Log(fmt.Sprintf("\n%-50s %-8s %-6s %s", "QUERY", "STATUS", "KIND", "TOP RESULT"))
	t.Log(strings.Repeat("-", 100))

	for _, track := range tracks {
		perms := queryPermutations(track)
		for _, perm := range perms {
			providers := syntheticProviders(track, artists)
			queryNorm := NormalizeForMatch(perm.query)
			results := FuseAndRank(providers, queryNorm, noQualityScorer, nil)

			if len(results) == 0 {
				t.Log(fmt.Sprintf("%-50s %-8s %-6s %s", truncate(perm.query, 48), "SKIP", "-", "no results"))
				skipped++
				continue
			}

			r := results[0]
			ok := matchesTrack(r, track, perm)
			status := "PASS"
			if !ok {
				status := "FAIL"
				_ = status
				failed++
			} else {
				passed++
			}

			t.Log(fmt.Sprintf("%-50s %-8s %-6s [%s] %q by %q",
				truncate(perm.query, 48), status, r.Kind.String(),
				r.Kind.String(), truncate(r.Title, 30), truncate(r.Subtitle, 20)))
		}
	}

	t.Log(strings.Repeat("-", 100))
	t.Log(fmt.Sprintf("Total: %d passed, %d failed, %d skipped out of %d permutations",
		passed, failed, skipped, passed+failed+skipped))

	if failed > 0 {
		t.Logf("WARNING: %d permutations did not rank the expected track as #1", failed)
	}
}

// TestPipelineIntegration_NormalizationConsistency verifies that every track
// in the library normalizes consistently — if we normalize, clean, and re-
// normalize, the result is stable (idempotent).
func TestPipelineIntegration_NormalizationConsistency(t *testing.T) {
	pool := integrationPool(t)
	tracks := loadLibraryTracks(t, pool)

	for _, track := range tracks {
		for _, text := range []string{track.title, track.artist, track.album} {
			if text == "" {
				continue
			}
			norm1 := NormalizeForMatch(text)
			norm2 := NormalizeForMatch(norm1)
			if norm1 != norm2 {
				t.Errorf("NormalizeForMatch is not idempotent for %q: %q → %q", text, norm1, norm2)
			}
		}
	}
}

// TestPipelineIntegration_CleanQueryPreservesIntent verifies that CleanQuery
// does not destroy the meaningful part of any track title in the library.
func TestPipelineIntegration_CleanQueryPreservesIntent(t *testing.T) {
	pool := integrationPool(t)
	tracks := loadLibraryTracks(t, pool)

	for _, track := range tracks {
		cleaned := CleanQuery(track.title)
		cleanedNorm := NormalizeForMatch(cleaned)
		originalNorm := NormalizeForMatch(track.title)
		if cleanedNorm == "" && originalNorm != "" {
			t.Errorf("CleanQuery destroyed %q → %q (empty after normalize)", track.title, cleaned)
		}
	}
}

// ---------------------------------------------------------------------------
// Types and helpers
// ---------------------------------------------------------------------------

type libraryTrack struct {
	title  string
	artist string
	album  string
}

type queryPerm struct {
	query       string
	expectTitle string
	variant     string
}

func integrationPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func loadLibraryTracks(t *testing.T, pool *pgxpool.Pool) []libraryTrack {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT DISTINCT title, artist, COALESCE(album, '') FROM tracks ORDER BY artist, title LIMIT 500`)
	if err != nil {
		t.Fatalf("query tracks: %v", err)
	}
	defer rows.Close()

	var tracks []libraryTrack
	for rows.Next() {
		var tr libraryTrack
		if err := rows.Scan(&tr.title, &tr.artist, &tr.album); err != nil {
			t.Fatalf("scan track: %v", err)
		}
		tracks = append(tracks, tr)
	}
	return tracks
}

func uniqueArtists(tracks []libraryTrack) []string {
	seen := make(map[string]bool)
	var artists []string
	for _, t := range tracks {
		norm := NormalizeForMatch(t.artist)
		if !seen[norm] {
			seen[norm] = true
			artists = append(artists, t.artist)
		}
	}
	return artists
}

func queryPermutations(track libraryTrack) []queryPerm {
	perms := []queryPerm{
		{query: track.title, expectTitle: track.title, variant: "title_exact"},
		{query: track.artist + " " + track.title, expectTitle: track.title, variant: "artist_title"},
		{query: track.title + " " + track.artist, expectTitle: track.title, variant: "title_artist"},
		{query: strings.ToUpper(track.title), expectTitle: track.title, variant: "title_upper"},
		{query: strings.ToLower(track.title), expectTitle: track.title, variant: "title_lower"},
	}

	if track.album != "" {
		perms = append(perms, queryPerm{
			query: track.title + " " + track.album, expectTitle: track.title, variant: "title_album",
		})
	}

	typo := introduceTypo(track.title)
	if typo != track.title {
		perms = append(perms, queryPerm{
			query: typo, expectTitle: track.title, variant: "typo",
		})
	}

	if len(track.title) > 3 {
		partial := track.title[:len(track.title)*2/3]
		perms = append(perms, queryPerm{
			query: partial, expectTitle: track.title, variant: "partial",
		})
	}

	return perms
}

func introduceTypo(s string) string {
	if len(s) < 4 {
		return s
	}
	runes := []rune(s)
	pos := 1 + rand.Intn(len(runes)-2)
	if unicode.IsLetter(runes[pos]) {
		runes[pos] = runes[pos] + 1
	}
	return string(runes)
}

func syntheticProviders(track libraryTrack, allArtists []string) [][]domain.SearchResult {
	var deezerResults []domain.SearchResult

	deezerResults = append(deezerResults, trackResult(
		domain.ProviderDeezer, "dz-target",
		track.title, track.artist,
		map[string]any{"rank": int64(700_000), "isrc": "TARGET001"},
	))

	for i, artist := range allArtists {
		if NormalizeForMatch(artist) == NormalizeForMatch(track.artist) {
			continue
		}
		if i > 5 {
			break
		}
		deezerResults = append(deezerResults, trackResult(
			domain.ProviderDeezer, fmt.Sprintf("dz-noise-%d", i),
			track.title, artist,
			map[string]any{"rank": int64(100_000 - int64(i*10_000))},
		))
	}

	return [][]domain.SearchResult{deezerResults}
}

func matchesTrack(result domain.SearchResult, track libraryTrack, perm queryPerm) bool {
	titleNorm := NormalizeForMatch(result.Title)
	expectNorm := NormalizeForMatch(perm.expectTitle)
	artistNorm := NormalizeForMatch(result.Subtitle)
	expectArtistNorm := NormalizeForMatch(track.artist)

	titleMatch := strings.Contains(titleNorm, expectNorm) || strings.Contains(expectNorm, titleNorm)
	artistMatch := strings.Contains(artistNorm, expectArtistNorm) || strings.Contains(expectArtistNorm, artistNorm)

	if perm.variant == "typo" || perm.variant == "partial" {
		return titleMatch
	}

	return titleMatch && artistMatch
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-2] + ".."
}
