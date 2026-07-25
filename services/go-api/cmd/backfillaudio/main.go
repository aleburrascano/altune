package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	acquisitionService "altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/adapters/storage"
	"altune/go-api/internal/catalog/domain"
	catalogPorts "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/database"

	"github.com/jackc/pgx/v5/pgxpool"
)

var extensions = []string{".mp3", ".m4a", ".opus", ".ogg"}

type options struct {
	album string
	user  string
	apply  bool
	list   bool
	verify bool
}

type candidate struct {
	trackID domain.TrackId
	userID  shared.UserId
	title     string
	artist    string
	album     string
	storedRef string
}

type outcome struct {
	candidate candidate
	ref       string
	tried     []string
	err       error
}

func main() {
	var opts options
	flag.StringVar(&opts.album, "album", "UNRELEASED", "album name to match (case-insensitive; empty matches every album)")
	flag.StringVar(&opts.user, "user", "", "restrict to one user id (default: every user)")
	flag.BoolVar(&opts.apply, "apply", false, "write the changes; without it the run only reports")
	flag.BoolVar(&opts.list, "list", false, "print every object key in storage and exit")
	flag.BoolVar(&opts.verify, "verify", false, "check ready tracks' stored refs instead of backfilling unready ones")
	flag.Parse()

	if err := run(opts); err != nil {
		fmt.Fprintf(os.Stderr, "backfillaudio: %v\n", err)
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

	audioStore, err := buildAudioStore(cfg)
	if err != nil {
		return err
	}

	if opts.list {
		return listStorage(ctx, audioStore)
	}

	pool, err := database.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()

	candidates, err := loadCandidates(ctx, pool, opts)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		fmt.Println("no unplayable tracks matched the filter")
		return nil
	}

	repo := persistence.NewPgxTrackRepository(pool)

	if opts.verify {
		return verifyRefs(ctx, repo, audioStore, candidates, opts.apply)
	}

	outcomes := make([]outcome, 0, len(candidates))
	for _, c := range candidates {
		outcomes = append(outcomes, reconcile(ctx, repo, audioStore, c, opts.apply))
	}
	report(outcomes, opts.apply)
	return nil
}

const missingAudioReason = "audio file missing from storage"

func verifyRefs(
	ctx context.Context,
	repo trackWriter,
	store catalogPorts.AudioStore,
	candidates []candidate,
	apply bool,
) error {
	var broken int
	for _, c := range candidates {
		exists, err := store.Exists(ctx, c.storedRef)
		if err != nil {
			return fmt.Errorf("check %q: %w", c.storedRef, err)
		}
		if exists {
			continue
		}
		broken++
		fmt.Printf("BROKEN  %s - %s [%s]\n          ref %s\n", c.artist, c.title, c.album, c.storedRef)
		if !apply {
			continue
		}
		if err := markFailed(ctx, repo, c); err != nil {
			return fmt.Errorf("repair %s: %w", c.trackID, err)
		}
		fmt.Printf("          -> marked failed, eligible for re-acquisition\n")
	}
	fmt.Printf("\n%d ready tracks checked, %d point at a missing object\n", len(candidates), broken)
	if !apply && broken > 0 {
		fmt.Println("dry run - re-run with -apply to mark these for re-acquisition")
	}
	return nil
}

func markFailed(ctx context.Context, repo trackWriter, c candidate) error {
	track, err := repo.GetByID(ctx, c.trackID, c.userID)
	if err != nil {
		return fmt.Errorf("load track: %w", err)
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	if err := track.MarkFailed(missingAudioReason); err != nil {
		return fmt.Errorf("mark failed: %w", err)
	}
	return repo.Update(ctx, track)
}

func buildAudioStore(cfg *config.Config) (catalogPorts.AudioStore, error) {
	if cfg.HasOCIS3() {
		return storage.NewObjectStorageAudioStore(
			cfg.OCIS3Endpoint,
			cfg.OCIS3AccessKey,
			cfg.OCIS3SecretKey,
			cfg.OCIS3Bucket,
			cfg.OCIS3Region,
		)
	}
	if cfg.MusicDir != "" {
		return storage.NewFilesystemAudioStore(cfg.MusicDir), nil
	}
	return nil, errors.New("no audio store configured: set OCI_S3_* or MUSIC_DIR")
}

func listStorage(ctx context.Context, store catalogPorts.AudioStore) error {
	lister, ok := store.(catalogPorts.AudioLister)
	if !ok {
		return errors.New("the configured audio store cannot list objects")
	}
	refs, err := lister.List(ctx, "")
	if err != nil {
		return fmt.Errorf("list storage: %w", err)
	}
	for _, ref := range refs {
		fmt.Println(ref)
	}
	fmt.Printf("\n%d objects\n", len(refs))
	return nil
}

func loadCandidates(ctx context.Context, pool *pgxpool.Pool, opts options) ([]candidate, error) {
	gate := "(acquisition_status <> 'ready' OR audio_ref IS NULL)"
	if opts.verify {
		gate = "(acquisition_status = 'ready' AND audio_ref IS NOT NULL)"
	}
	query := `SELECT id, user_id, title, artist, COALESCE(album, ''), COALESCE(audio_ref, '') FROM tracks
	          WHERE ` + gate + `
	            AND ($1 = '' OR upper(COALESCE(album, '')) = upper($1))
	            AND ($2 = '' OR user_id::text = $2)
	          ORDER BY user_id, artist, album, title`

	rows, err := pool.Query(ctx, query, opts.album, opts.user)
	if err != nil {
		return nil, fmt.Errorf("query candidates: %w", err)
	}
	defer rows.Close()

	var out []candidate
	for rows.Next() {
		var c candidate
		var id, userID string
		if err := rows.Scan(&id, &userID, &c.title, &c.artist, &c.album, &c.storedRef); err != nil {
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

func candidateRefs(c candidate) []string {
	owned := acquisitionService.TrackRef{
		UserID: c.userID.String(),
		Title:  c.title,
		Artist: c.artist,
		Album:  c.album,
	}
	flat := owned
	flat.UserID = ""

	refs := make([]string, 0, 2*len(extensions))
	for _, ext := range extensions {
		refs = append(refs, acquisitionService.BuildAudioRef(owned, "audio"+ext))
	}
	for _, ext := range extensions {
		refs = append(refs, strings.TrimPrefix(acquisitionService.BuildAudioRef(flat, "audio"+ext), "/"))
	}
	return refs
}

func locate(ctx context.Context, store catalogPorts.AudioStore, refs []string) (string, error) {
	for _, ref := range refs {
		exists, err := store.Exists(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("check %q: %w", ref, err)
		}
		if exists {
			return ref, nil
		}
	}
	return "", nil
}

type trackWriter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
}

func reconcile(ctx context.Context, repo trackWriter, store catalogPorts.AudioStore, c candidate, apply bool) outcome {
	refs := candidateRefs(c)
	ref, err := locate(ctx, store, refs)
	if err != nil || ref == "" {
		return outcome{candidate: c, tried: refs, err: err}
	}
	if !apply {
		return outcome{candidate: c, ref: ref}
	}
	return outcome{candidate: c, ref: ref, err: markReady(ctx, repo, c, ref)}
}

func markReady(ctx context.Context, repo trackWriter, c candidate, ref string) error {
	track, err := repo.GetByID(ctx, c.trackID, c.userID)
	if err != nil {
		return fmt.Errorf("load track: %w", err)
	}
	if track == nil {
		return errors.New("track disappeared")
	}
	if err := track.MarkReady(ref); err != nil {
		return fmt.Errorf("mark ready: %w", err)
	}
	if err := repo.Update(ctx, track); err != nil {
		return fmt.Errorf("persist: %w", err)
	}
	return nil
}

func report(outcomes []outcome, apply bool) {
	var matched, failed int
	for _, o := range outcomes {
		label := fmt.Sprintf("%s - %s [%s]", o.candidate.artist, o.candidate.title, o.candidate.album)
		switch {
		case o.err != nil:
			failed++
			fmt.Printf("ERROR   %s: %v\n", label, o.err)
		case o.ref == "":
			fmt.Printf("MISSING %s\n", label)
			for _, ref := range o.tried {
				fmt.Printf("          tried %s\n", ref)
			}
		default:
			matched++
			fmt.Printf("%-7s %s -> %s\n", verb(apply), label, o.ref)
		}
	}
	fmt.Printf("\n%d matched, %d missing, %d errored, %d total\n",
		matched, len(outcomes)-matched-failed, failed, len(outcomes))
	if !apply && matched > 0 {
		fmt.Println("dry run - re-run with -apply to write these")
	}
}

func verb(apply bool) string {
	if apply {
		return "READY"
	}
	return "WOULD"
}
