package main

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"fmt"
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
	return textnorm.NormalizeForMatch(domain.CleanSearchKeys(name, "", "").Title)
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
	rows, err := s.pool.Query(ctx, heldTracksSQL, userID)
	if err != nil {
		return nil, fmt.Errorf("query held tracks: %w", err)
	}
	defer rows.Close()
	var tracks []heldTrack
	for rows.Next() {
		var held heldTrack
		if err := rows.Scan(&held.ID, &held.UserID, &held.Artist, &held.Album, &held.Title); err != nil {
			return nil, fmt.Errorf("scan held track: %w", err)
		}
		tracks = append(tracks, held)
	}
	return newHeldIndex(tracks), rows.Err()
}
