package main

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	discoveryDomain "altune/go-api/internal/discovery/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

// perTrackTimeout caps the wall time spent re-resolving one cover. The chained
// resolver has its own internal budget; this is a belt-and-braces guard so a
// single stuck provider cannot stall the whole backfill.
const perTrackTimeout = 30 * time.Second

// emptyArtHash is the md5 of an empty body: providers hand it back as a
// "no artwork" placeholder, so a stored URL carrying it is really blank.
const emptyArtHash = "d41d8cd98f00b204e9800998ecf8427e"

type options struct {
	user  string
	apply bool
}

type candidate struct {
	trackID    domain.TrackId
	userID     shared.UserId
	title      string
	artist     string
	oldArtwork string
}

type trackWriter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
}

// artworkResolver is the slice of the discovery TaggingArtworkResolver this tool
// needs. Kept local so heal() can be unit-tested with a fake.
type artworkResolver interface {
	ResolveTagged(ctx context.Context, kind discoveryDomain.ResultKind, title, subtitle, mbid string) (url string, source discoveryDomain.ProviderKey, err error)
}

func main() {
	var opts options
	flag.StringVar(&opts.user, "user", "", "restrict to one user id (default: every user)")
	flag.BoolVar(&opts.apply, "apply", false, "write the changes; without it the run only reports")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "backfillartwork: %v\n", err)
		os.Exit(1)
	}
}

func run(opts options) error {
	ctx := context.Background()

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
		fmt.Println("no tracks have a blank or suspect artwork_url")
		return nil
	}

	resolver := app.BuildArtworkChain(cfg)
	return heal(ctx, persistence.NewPgxTrackRepository(pool), resolver, candidates, opts.apply)
}

// isBlankOrSuspect reports whether a stored artwork_url is missing or a known
// placeholder that should be re-resolved. Kept in sync with the SQL gate in
// loadCandidates so the Go and DB views of "suspect" agree.
func isBlankOrSuspect(url string) bool {
	trimmed := strings.TrimSpace(url)
	if trimmed == "" {
		return true
	}
	if strings.Contains(trimmed, emptyArtHash) {
		return true
	}
	// Deezer's artist placeholder path (empty artist id) — see providers.IsDeezerPlaceholder.
	return strings.Contains(trimmed, "/images/artist//")
}

func loadCandidates(ctx context.Context, pool *pgxpool.Pool, user string) ([]candidate, error) {
	rows, err := pool.Query(ctx,
		`SELECT id, user_id, title, artist, COALESCE(artwork_url, '') FROM tracks
		 WHERE (COALESCE(TRIM(artwork_url), '') = ''
		        OR artwork_url LIKE '%/images/artist//%'
		        OR artwork_url LIKE '%`+emptyArtHash+`%')
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
		if err := rows.Scan(&id, &userID, &c.title, &c.artist, &c.oldArtwork); err != nil {
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

func heal(ctx context.Context, repo trackWriter, resolver artworkResolver, candidates []candidate, apply bool) error {
	var fixed, unresolved int
	for _, c := range candidates {
		newURL := resolveCover(ctx, resolver, c)
		if newURL == "" || isBlankOrSuspect(newURL) {
			unresolved++
			fmt.Printf("NONE    %s - %s (no better cover found)\n", c.artist, c.title)
			continue
		}
		if newURL == c.oldArtwork {
			unresolved++
			fmt.Printf("SAME    %s - %s (resolver returned the stored cover)\n", c.artist, c.title)
			continue
		}

		fmt.Printf("%-7s %s - %s\n          old: %s\n          new: %s\n",
			verb(apply), c.artist, c.title, displayURL(c.oldArtwork), newURL)
		if !apply {
			fixed++
			continue
		}
		if err := setArtwork(ctx, repo, c, newURL); err != nil {
			return fmt.Errorf("heal %s: %w", c.trackID, err)
		}
		fixed++
	}

	fmt.Printf("\n%d %s, %d left unresolved, %d total\n", fixed, fixedWord(apply), unresolved, len(candidates))
	if !apply && fixed > 0 {
		fmt.Println("dry run - re-run with -apply to write these")
	}
	return nil
}

func resolveCover(ctx context.Context, resolver artworkResolver, c candidate) string {
	ctx, cancel := context.WithTimeout(ctx, perTrackTimeout)
	defer cancel()
	url, _, err := resolver.ResolveTagged(ctx, discoveryDomain.ResultKindTrack, c.title, c.artist, "")
	if err != nil {
		return ""
	}
	return url
}

func setArtwork(ctx context.Context, repo trackWriter, c candidate, url string) error {
	track, err := repo.GetByID(ctx, c.trackID, c.userID)
	if err != nil {
		return fmt.Errorf("load track: %w", err)
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	track.ArtworkURL = &url
	return repo.Update(ctx, track)
}

func displayURL(url string) string {
	if strings.TrimSpace(url) == "" {
		return "(blank)"
	}
	return url
}

func verb(apply bool) string {
	if apply {
		return "SET"
	}
	return "WOULD"
}

func fixedWord(apply bool) string {
	if apply {
		return "fixed"
	}
	return "would be fixed"
}
