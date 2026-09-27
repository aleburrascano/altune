package commands

import (
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func adminDatabaseURL(t *testing.T) *url.URL {
	t.Helper()
	raw := os.Getenv("DATABASE_URL")
	if raw == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	return u
}

func freshDatabase(t *testing.T, base *url.URL, name string) string {
	t.Helper()
	ctx := context.Background()

	adminURL := *base
	adminURL.Path = "/postgres"
	adminPool, err := pgxpool.New(ctx, adminURL.String())
	if err != nil {
		t.Fatalf("connect to admin database: %v", err)
	}
	defer adminPool.Close()

	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, name)); err != nil {
		t.Fatalf("drop existing %s: %v", name, err)
	}
	if _, err := adminPool.Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s`, name)); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	t.Cleanup(func() {
		cleanupPool, err := pgxpool.New(context.Background(), adminURL.String())
		if err != nil {
			return
		}
		defer cleanupPool.Close()
		_, _ = cleanupPool.Exec(context.Background(), fmt.Sprintf(`DROP DATABASE IF EXISTS %s`, name))
	})

	dbURL := *base
	dbURL.Path = "/" + name

	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	migrationsDir := filepath.Join(root, "..", "..", "..", "migrations")
	entries, err := filepath.Glob(filepath.Join(migrationsDir, "*.sql"))
	if err != nil || len(entries) == 0 {
		t.Fatalf("glob migrations: %v (found %d)", err, len(entries))
	}
	for _, f := range entries {
		cmd := exec.Command("psql", dbURL.String(), "-v", "ON_ERROR_STOP=1", "-q", "-f", f)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("apply %s: %v\n%s", f, err, out)
		}
	}

	pool, err := pgxpool.New(ctx, dbURL.String())
	if err != nil {
		t.Fatalf("connect to %s: %v", name, err)
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA auth; CREATE TABLE auth.users (id uuid PRIMARY KEY, email text)`); err != nil {
		pool.Close()
		t.Fatalf("create identity store in %s: %v", name, err)
	}
	pool.Close()

	return dbURL.String()
}

func TestRunPromoteStaging_PromotesSkipsAndLeavesProdUntouched(t *testing.T) {
	sharedtest.RequireIntegration(t)
	base := adminDatabaseURL(t)
	prodURL := freshDatabase(t, base, "promote_it_prod")
	stagingURL := freshDatabase(t, base, "promote_it_staging")
	t.Setenv("STAGING_DATABASE_URL", stagingURL)

	ctx := context.Background()
	prodPool, err := pgxpool.New(ctx, prodURL)
	if err != nil {
		t.Fatalf("connect prod: %v", err)
	}
	defer prodPool.Close()
	stagingPool, err := pgxpool.New(ctx, stagingURL)
	if err != nil {
		t.Fatalf("connect staging: %v", err)
	}
	defer stagingPool.Close()

	prodUser := uuid.New()
	stagingUser := uuid.New()
	trackNew := uuid.New()
	trackDup := uuid.New()
	trackObjExists := uuid.New()
	trackKeep := uuid.New()

	if _, err := prodPool.Exec(ctx, `INSERT INTO auth.users (id, email) VALUES ($1, 'Op@Example.com')`, prodUser); err != nil {
		t.Fatalf("seed prod account: %v", err)
	}
	if _, err := prodPool.Exec(ctx,
		`INSERT INTO tracks (id, user_id, title, artist, dedup_key, audio_ref, acquisition_status) VALUES ($1, $2, 'Already there', 'A', 'dk-existing', $3, 'ready')`,
		trackKeep, prodUser, prodUser.String()+"/A/Alb/kept.opus"); err != nil {
		t.Fatalf("seed prod track: %v", err)
	}

	if _, err := stagingPool.Exec(ctx, `INSERT INTO auth.users (id, email) VALUES ($1, 'op@example.com')`, stagingUser); err != nil {
		t.Fatalf("seed staging account: %v", err)
	}
	stagingRefs := map[uuid.UUID]string{
		trackNew:       "staging/" + stagingUser.String() + "/A/Alb/new.opus",
		trackDup:       "staging/" + stagingUser.String() + "/A/Alb/dup.opus",
		trackObjExists: "staging/" + stagingUser.String() + "/A/Alb/collide.opus",
	}
	dedupKeys := map[uuid.UUID]string{
		trackNew:       "dk-new",
		trackDup:       "dk-existing",
		trackObjExists: "dk-collide",
	}
	for _, id := range []uuid.UUID{trackNew, trackDup, trackObjExists} {
		if _, err := stagingPool.Exec(ctx,
			`INSERT INTO tracks (id, user_id, title, artist, dedup_key, audio_ref, acquisition_status) VALUES ($1, $2, 'Song', 'A', $3, $4, 'ready')`,
			id, stagingUser, dedupKeys[id], stagingRefs[id]); err != nil {
			t.Fatalf("seed staging track: %v", err)
		}
	}

	musicDir := t.TempDir()
	writeFixture := func(rel, contents string) {
		full := filepath.Join(musicDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("mkdir for fixture: %v", err)
		}
		if err := os.WriteFile(full, []byte(contents), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}
	writeFixture(prodUser.String()+"/A/Alb/kept.opus", "kept-bytes")
	writeFixture("staging/"+stagingUser.String()+"/A/Alb/new.opus", "new-bytes")
	writeFixture("staging/"+stagingUser.String()+"/A/Alb/dup.opus", "dup-bytes")
	writeFixture("staging/"+stagingUser.String()+"/A/Alb/collide.opus", "collide-staging-bytes")
	writeFixture(prodUser.String()+"/A/Alb/collide.opus", "collide-prod-bytes")

	cfg := &config.Config{DatabaseURL: prodURL, DBPoolMaxConns: 5, MusicDir: musicDir}

	if err := runPromoteStaging(cfg, true); err != nil {
		t.Fatalf("runPromoteStaging: %v", err)
	}

	var gotUserID uuid.UUID
	var gotRef string
	if err := prodPool.QueryRow(ctx, `SELECT user_id, audio_ref FROM tracks WHERE id = $1`, trackNew).Scan(&gotUserID, &gotRef); err != nil {
		t.Fatalf("read promoted row: %v", err)
	}
	if gotUserID != prodUser {
		t.Errorf("promoted user_id = %s, want %s", gotUserID, prodUser)
	}
	wantRef := prodUser.String() + "/A/Alb/new.opus"
	if gotRef != wantRef {
		t.Errorf("promoted audio_ref = %q, want %q", gotRef, wantRef)
	}
	gotBytes, err := os.ReadFile(filepath.Join(musicDir, wantRef))
	if err != nil {
		t.Fatalf("read promoted object: %v", err)
	}
	if string(gotBytes) != "new-bytes" {
		t.Errorf("promoted object contents = %q, want %q", gotBytes, "new-bytes")
	}

	var dupCount int
	if err := prodPool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id = $1`, trackDup).Scan(&dupCount); err != nil {
		t.Fatalf("count dup row: %v", err)
	}
	if dupCount != 0 {
		t.Errorf("dedup-skipped track landed in prod: count = %d", dupCount)
	}

	var collideCount int
	if err := prodPool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id = $1`, trackObjExists).Scan(&collideCount); err != nil {
		t.Fatalf("count collide row: %v", err)
	}
	if collideCount != 0 {
		t.Errorf("object-exists-skipped track landed in prod: count = %d", collideCount)
	}
	collideProdBytes, err := os.ReadFile(filepath.Join(musicDir, prodUser.String()+"/A/Alb/collide.opus"))
	if err != nil {
		t.Fatalf("read pre-existing prod object at the colliding ref: %v", err)
	}
	if string(collideProdBytes) != "collide-prod-bytes" {
		t.Errorf("pre-existing prod object at the colliding ref was overwritten: got %q", collideProdBytes)
	}

	var keepDedupKey, keepAudioRef string
	if err := prodPool.QueryRow(ctx, `SELECT dedup_key, audio_ref FROM tracks WHERE id = $1`, trackKeep).Scan(&keepDedupKey, &keepAudioRef); err != nil {
		t.Fatalf("read pre-existing prod row: %v", err)
	}
	if keepDedupKey != "dk-existing" || keepAudioRef != prodUser.String()+"/A/Alb/kept.opus" {
		t.Errorf("pre-existing prod row changed: dedup_key=%q audio_ref=%q", keepDedupKey, keepAudioRef)
	}

	if err := runPromoteStaging(cfg, true); err != nil {
		t.Fatalf("second runPromoteStaging: %v", err)
	}
	var againCount int
	if err := prodPool.QueryRow(ctx, `SELECT count(*) FROM tracks WHERE id = $1`, trackNew).Scan(&againCount); err != nil {
		t.Fatalf("count promoted row after rerun: %v", err)
	}
	if againCount != 1 {
		t.Errorf("rerun duplicated the promotion: count = %d, want 1", againCount)
	}
}
