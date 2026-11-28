package persistence

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/database"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.UnreleasedStore = (*PostgresUnreleasedStore)(nil)

type PostgresUnreleasedStore struct {
	pool *pgxpool.Pool
}

func NewPostgresUnreleasedStore(pool *pgxpool.Pool) *PostgresUnreleasedStore {
	return &PostgresUnreleasedStore{pool: pool}
}

func encodeUnreleasedSong(song domain.UnreleasedSong) ([]byte, error) {
	b, err := json.Marshal(song)
	if err != nil {
		return nil, fmt.Errorf("encode unreleased song: %w", err)
	}
	return b, nil
}

func decodeUnreleasedSong(raw []byte) (domain.UnreleasedSong, error) {
	var song domain.UnreleasedSong
	if err := json.Unmarshal(raw, &song); err != nil {
		return domain.UnreleasedSong{}, fmt.Errorf("decode unreleased song: %w", err)
	}
	return song, nil
}

func encodeKnownIDs(ids map[domain.ProviderName]string) ([]byte, error) {
	byKey := make(map[string]string, len(ids))
	for p, id := range ids {
		byKey[p.String()] = id
	}
	b, err := json.Marshal(byKey)
	if err != nil {
		return nil, fmt.Errorf("encode known ids: %w", err)
	}
	return b, nil
}

func decodeKnownIDs(raw []byte) (map[domain.ProviderName]string, error) {
	var byKey map[string]string
	if err := json.Unmarshal(raw, &byKey); err != nil {
		return nil, fmt.Errorf("decode known ids: %w", err)
	}
	ids := make(map[domain.ProviderName]string, len(byKey))
	for key, id := range byKey {
		p, err := domain.ParseProviderName(key)
		if err != nil {
			return nil, fmt.Errorf("decode known ids: %w", err)
		}
		ids[p] = id
	}
	return ids, nil
}

const recordUnreleasedArtistSQL = `
	INSERT INTO unreleased_artists (provider, external_id, name, known_ids)
	VALUES ($1, $2, $3, $4)
	ON CONFLICT (provider, external_id) DO UPDATE
	SET name = EXCLUDED.name,
	    known_ids = EXCLUDED.known_ids,
	    last_requested_at = CASE
	        WHEN unreleased_artists.last_requested_at < now() - interval '1 hour' THEN now()
	        ELSE unreleased_artists.last_requested_at
	    END`

func (s *PostgresUnreleasedStore) Record(ctx context.Context, key ports.UnreleasedArtistKey, name string, knownIDs map[domain.ProviderName]string) error {
	known, err := encodeKnownIDs(knownIDs)
	if err != nil {
		return err
	}
	if _, err := s.pool.Exec(ctx, recordUnreleasedArtistSQL, key.Provider.String(), key.ExternalID, name, known); err != nil {
		return fmt.Errorf("record unreleased artist: %w", err)
	}
	return nil
}

func (s *PostgresUnreleasedStore) Read(ctx context.Context, key ports.UnreleasedArtistKey) ([]domain.UnreleasedSong, bool, bool, int, error) {
	complete, built, version, err := s.readBuildState(ctx, key)
	if err != nil || !built {
		return nil, false, false, 0, err
	}
	songs, err := s.readSongs(ctx, key)
	if err != nil {
		return nil, false, false, 0, err
	}
	return songs, complete, true, version, nil
}

func (s *PostgresUnreleasedStore) readBuildState(ctx context.Context, key ports.UnreleasedArtistKey) (complete, built bool, version int, err error) {
	var builtAt *time.Time
	err = s.pool.QueryRow(ctx,
		`SELECT complete, built_at, build_version FROM unreleased_artists WHERE provider = $1 AND external_id = $2`,
		key.Provider.String(), key.ExternalID,
	).Scan(&complete, &builtAt, &version)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, false, 0, nil
	}
	if err != nil {
		return false, false, 0, fmt.Errorf("read unreleased artist: %w", err)
	}
	return complete, builtAt != nil, version, nil
}

func (s *PostgresUnreleasedStore) readSongs(ctx context.Context, key ports.UnreleasedArtistKey) ([]domain.UnreleasedSong, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT song FROM unreleased_songs WHERE provider = $1 AND external_id = $2 ORDER BY position`,
		key.Provider.String(), key.ExternalID,
	)
	if err != nil {
		return nil, fmt.Errorf("read unreleased songs: %w", err)
	}
	songs, err := pgx.CollectRows(rows, scanSong)
	if err != nil {
		return nil, fmt.Errorf("read unreleased songs: %w", err)
	}
	return songs, nil
}

func scanSong(row pgx.CollectableRow) (domain.UnreleasedSong, error) {
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return domain.UnreleasedSong{}, fmt.Errorf("scan unreleased song: %w", err)
	}
	return decodeUnreleasedSong(raw)
}

const claimUnreleasedBuildSQL = `
	UPDATE unreleased_artists
	SET built_at = $3, complete = $4, song_count = $5, build_version = $6
	WHERE provider = $1 AND external_id = $2 AND (built_at IS NULL OR built_at < $3)`

func (s *PostgresUnreleasedStore) Write(ctx context.Context, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, complete bool, startedAt time.Time, buildVersion int) error {
	encoded, err := encodeUnreleasedSongs(songs)
	if err != nil {
		return err
	}
	err = database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		claimed, err := claimBuild(ctx, tx, key, len(songs), complete, startedAt, buildVersion)
		if err != nil || !claimed {
			return err
		}
		return replaceSongs(ctx, tx, key, songs, encoded)
	})
	if err != nil {
		return fmt.Errorf("write unreleased artist %s/%s: %w", key.Provider, key.ExternalID, err)
	}
	return nil
}

func claimBuild(ctx context.Context, tx pgx.Tx, key ports.UnreleasedArtistKey, songCount int, complete bool, startedAt time.Time, buildVersion int) (bool, error) {
	tag, err := tx.Exec(ctx, claimUnreleasedBuildSQL, key.Provider.String(), key.ExternalID, startedAt, complete, songCount, buildVersion)
	if err != nil {
		return false, fmt.Errorf("update unreleased artist: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func encodeUnreleasedSongs(songs []domain.UnreleasedSong) ([][]byte, error) {
	encoded := make([][]byte, len(songs))
	for i, song := range songs {
		raw, err := encodeUnreleasedSong(song)
		if err != nil {
			return nil, err
		}
		encoded[i] = raw
	}
	return encoded, nil
}

func replaceSongs(ctx context.Context, tx pgx.Tx, key ports.UnreleasedArtistKey, songs []domain.UnreleasedSong, encoded [][]byte) error {
	if _, err := tx.Exec(ctx,
		`DELETE FROM unreleased_songs WHERE provider = $1 AND external_id = $2`,
		key.Provider.String(), key.ExternalID,
	); err != nil {
		return fmt.Errorf("clear unreleased songs: %w", err)
	}
	batch := &pgx.Batch{}
	for i, song := range songs {
		batch.Queue(
			`INSERT INTO unreleased_songs (provider, external_id, position, title, song, search_key) VALUES ($1, $2, $3, $4, $5, $6)`,
			key.Provider.String(), key.ExternalID, i, song.Title, encoded[i], domain.TitleKey(song.Title),
		)
	}
	return sendAll(ctx, tx, batch, len(songs))
}

func sendAll(ctx context.Context, tx pgx.Tx, batch *pgx.Batch, n int) error {
	results := tx.SendBatch(ctx, batch)
	for range n {
		if _, err := results.Exec(); err != nil {
			_ = results.Close()
			return fmt.Errorf("insert unreleased song: %w", err)
		}
	}
	if err := results.Close(); err != nil {
		return fmt.Errorf("insert unreleased songs: %w", err)
	}
	return nil
}

const searchUnreleasedSQL = `
	SELECT a.provider, a.external_id, a.name, s.song, similarity(s.search_key, $1)
	FROM unreleased_songs s
	JOIN unreleased_artists a ON a.provider = s.provider AND a.external_id = s.external_id
	WHERE s.search_key % $1
	ORDER BY similarity(s.search_key, $1) DESC, s.provider, s.external_id, s.position
	LIMIT $2`

func (s *PostgresUnreleasedStore) Search(ctx context.Context, query string, limit int) ([]ports.UnreleasedHit, error) {
	rows, err := s.pool.Query(ctx, searchUnreleasedSQL, domain.TitleKey(query), limit)
	if err != nil {
		return nil, fmt.Errorf("search unreleased songs: %w", err)
	}
	scored, err := pgx.CollectRows(rows, scanHit)
	if err != nil {
		return nil, fmt.Errorf("search unreleased songs: %w", err)
	}
	return rankHits(scored), nil
}

type scoredHit struct {
	ports.UnreleasedHit
	similarity float32
}

func rankHits(scored []scoredHit) []ports.UnreleasedHit {
	slices.SortStableFunc(scored, func(a, b scoredHit) int {
		return cmp.Or(cmp.Compare(b.similarity, a.similarity), cmp.Compare(b.Song.QualityRank(), a.Song.QualityRank()))
	})
	hits := make([]ports.UnreleasedHit, len(scored))
	for i, h := range scored {
		hits[i] = h.UnreleasedHit
	}
	return hits
}

func scanHit(row pgx.CollectableRow) (scoredHit, error) {
	var providerKey, externalID, name string
	var raw []byte
	var sim float32
	if err := row.Scan(&providerKey, &externalID, &name, &raw, &sim); err != nil {
		return scoredHit{}, fmt.Errorf("scan unreleased hit: %w", err)
	}
	provider, err := domain.ParseProviderName(providerKey)
	if err != nil {
		return scoredHit{}, fmt.Errorf("unreleased hit: %w", err)
	}
	song, err := decodeUnreleasedSong(raw)
	if err != nil {
		return scoredHit{}, err
	}
	hit := ports.UnreleasedHit{Artist: ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID}, ArtistName: name, Song: song}
	return scoredHit{UnreleasedHit: hit, similarity: sim}, nil
}

const dueUnreleasedSQL = `
	WITH picked AS (
		SELECT provider, external_id, attempted_at AS prior_attempt
		FROM unreleased_artists
		WHERE last_requested_at > $1::timestamptz - interval '30 days'
		  AND (attempted_at IS NULL OR attempted_at < $1::timestamptz - interval '1 hour')
		  AND (
			built_at IS NULL
			OR (complete AND song_count > 0 AND built_at < $1::timestamptz - interval '168 hours')
			OR (NOT (complete AND song_count > 0) AND built_at < $1::timestamptz - interval '1 hour')
		  )
		ORDER BY attempted_at ASC NULLS FIRST
		LIMIT $2
		FOR UPDATE SKIP LOCKED
	), stamped AS (
		UPDATE unreleased_artists a
		SET attempted_at = $1::timestamptz
		FROM picked p
		WHERE a.provider = p.provider AND a.external_id = p.external_id
		RETURNING a.provider, a.external_id, a.name, a.known_ids
	)
	SELECT s.provider, s.external_id, s.name, s.known_ids
	FROM stamped s
	JOIN picked p ON p.provider = s.provider AND p.external_id = s.external_id
	ORDER BY p.prior_attempt ASC NULLS FIRST`

func (s *PostgresUnreleasedStore) Due(ctx context.Context, now time.Time, limit int) ([]ports.UnreleasedDueArtist, error) {
	rows, err := s.pool.Query(ctx, dueUnreleasedSQL, now, limit)
	if err != nil {
		return nil, fmt.Errorf("due unreleased artists: %w", err)
	}
	due, err := pgx.CollectRows(rows, scanDueArtist)
	if err != nil {
		return nil, fmt.Errorf("due unreleased artists: %w", err)
	}
	return due, nil
}

func scanDueArtist(row pgx.CollectableRow) (ports.UnreleasedDueArtist, error) {
	var providerKey, externalID, name string
	var knownRaw []byte
	if err := row.Scan(&providerKey, &externalID, &name, &knownRaw); err != nil {
		return ports.UnreleasedDueArtist{}, fmt.Errorf("scan due unreleased artist: %w", err)
	}
	provider, err := domain.ParseProviderName(providerKey)
	if err != nil {
		return ports.UnreleasedDueArtist{}, fmt.Errorf("due unreleased artist: %w", err)
	}
	known, err := decodeKnownIDs(knownRaw)
	if err != nil {
		return ports.UnreleasedDueArtist{}, err
	}
	return ports.UnreleasedDueArtist{
		Key:      ports.UnreleasedArtistKey{Provider: provider, ExternalID: externalID},
		Name:     name,
		KnownIDs: known,
	}, nil
}
