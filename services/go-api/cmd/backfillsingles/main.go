package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"time"

	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const uniqueViolation = "23505"

type options struct {
	user  string
	apply bool
}

type candidate struct {
	trackID domain.TrackId
	userID  shared.UserId
	title   string
	artist  string
}

type trackWriter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
}

func main() {
	var opts options
	flag.StringVar(&opts.user, "user", "", "restrict to one user id (default: every user)")
	flag.BoolVar(&opts.apply, "apply", false, "write the changes; without it the run only reports")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "backfillsingles: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	candidates, err := loadCandidates(ctx, pool, opts.user)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Println("no tracks are missing an album")
		return nil
	}

	return retitle(ctx, persistence.NewPgxTrackRepository(pool), candidates, opts.apply)
}

func loadCandidates(ctx context.Context, pool *pgxpool.Pool, user string) ([]candidate, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist FROM tracks
		 WHERE COALESCE(TRIM(album), '') = ''
		   AND ($1 = '' OR user_id::text = $1)
		 ORDER BY user_id, artist, title`, user)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		var c candidate
		var id, userID string
		if err := rows.Scan(&id, &userID, &c.title, &c.artist); err != nil {
			return nil, fmt.Errorf("scan candidate: %w", err)
		}
		if c.trackID, err = domain.ParseTrackId(id); err != nil {
			return nil, fmt.Errorf("parse track id %q: %w", id, err)
		}
		if c.userID, err = shared.ParseUserId(userID); err != nil {
			return nil, fmt.Errorf("parse user id %q: %w", userID, err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func retitle(ctx context.Context, repo trackWriter, candidates []candidate, apply bool) error {
	var changed, collided int
	for _, c := range candidates {
		fmt.Printf("%-7s %s - %s -> album %q\n", verb(apply), c.artist, c.title, c.title)
		if !apply {
			changed++
			continue
		}
		switch err := setAlbumToTitle(ctx, repo, c); {
		case err == nil:
			changed++
		case isUniqueViolation(err):
			collided++
			fmt.Printf("          SKIPPED - a track with that title, artist and album already exists\n")
		default:
			return fmt.Errorf("retitle %s: %w", c.trackID, err)
		}
	}

	fmt.Printf("\n%d updated, %d skipped as duplicates, %d total\n", changed, collided, len(candidates))
	if !apply && changed > 0 {
		fmt.Println("dry run - re-run with -apply to write these")
	}
	return nil
}

func setAlbumToTitle(ctx context.Context, repo trackWriter, c candidate) error {
	track, err := repo.GetByID(ctx, c.trackID, c.userID)
	if err != nil {
		return fmt.Errorf("load track: %w", err)
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	track.SetAlbum("")
	return repo.Update(ctx, track)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == uniqueViolation
}

func verb(apply bool) string {
	if apply {
		return "SET"
	}
	return "WOULD"
}
