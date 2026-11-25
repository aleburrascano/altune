package main

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

type heldTrack struct {
	ID     string
	UserID string
	Artist string
	Album  string
	Title  string
}

type heldIndex map[string][]heldTrack

func heldKey(userID, artist string) string { return userID + "\x1f" + artistKey(artist) }

func newHeldIndex(tracks []heldTrack) heldIndex {
	index := heldIndex{}
	for _, held := range tracks {
		key := heldKey(held.UserID, held.Artist)
		index[key] = append(index[key], held)
	}
	return index
}

type heldKeys struct {
	Album string
	Title string
}

type holdings []heldKeys

func catalogKey(name string) string {
	return textnorm.NormalizeForMatch(catalog.CleanSearchKeys(name, "", "").Title)
}

func (h heldIndex) holdingsFor(t libraryTrack) holdings {
	var owned holdings
	for _, held := range h[heldKey(t.UserID, t.Artist)] {
		if held.ID == t.ID {
			continue
		}
		keys := heldKeys{Title: catalogKey(held.Title)}
		if !(libraryTrack{Album: held.Album}).hasUnreleasedAlbum() {
			keys.Album = catalogKey(held.Album)
		}
		owned = append(owned, keys)
	}
	return owned
}

const heldTracksSQL = `SELECT t.id::text, t.user_id::text, t.artist, COALESCE(t.album, ''), t.title
	FROM tracks t
	WHERE t.acquisition_status = 'ready'
	  AND ($1 = '' OR t.user_id::text = $1)`

func (s store) loadHeld(ctx context.Context, userID string) (heldIndex, error) {
	var tracks []heldTrack
	err := s.runTx(ctx, s.pool, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, heldTracksSQL, userID)
		if err != nil {
			return fmt.Errorf("query held tracks: %w", err)
		}
		tracks, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (heldTrack, error) {
			var held heldTrack
			err := row.Scan(&held.ID, &held.UserID, &held.Artist, &held.Album, &held.Title)
			return held, err
		})
		if err != nil {
			return fmt.Errorf("scan held tracks: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return newHeldIndex(tracks), nil
}
