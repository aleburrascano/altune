package app_test

import (
	"altune/go-api/internal/app"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type assembledRun struct {
	assembly *app.Assembly
	logs     *logging.Capture
}

func assembleLogging(t *testing.T, cfg *config.Config) assembledRun {
	t.Helper()
	return assembleWith(t, cfg, refuseEgress{})
}

func assembleWith(t *testing.T, cfg *config.Config, transport http.RoundTripper) assembledRun {
	t.Helper()
	return assembleOn(t, cfg, unreachablePool(t), transport)
}

func assembleOn(t *testing.T, cfg *config.Config, pool *pgxpool.Pool, transport http.RoundTripper) assembledRun {
	t.Helper()
	logs := logging.CaptureText(t, slog.LevelInfo)
	ctx, cancel := context.WithCancel(t.Context())
	assembly, err := app.Assemble(ctx, cfg, app.Handles{Pool: pool, Transport: transport, Tracker: testTracker(t)})
	if err != nil {
		t.Fatalf("Assemble: %v", err)
	}
	t.Cleanup(func() {
		cancel()
		assembly.Shutdown(context.Background())
	})
	return assembledRun{assembly: assembly, logs: logs}
}

func TestBackgroundJobs_TheAssemblyStartsTheHousekeepingJobsWithTheirOldLogLines(t *testing.T) {
	logged := assembleLogging(t, jwksConfig(t)).logs.String()

	for _, want := range []string{
		`level=INFO msg="orphaned audio reconcile started" interval=10m0s` + "\n",
		`level=INFO msg="deleted identity erasure started" interval=1h0m0s` + "\n",
		`level=INFO msg="discography event prune started" interval=24h0m0s` + "\n",
		`level=INFO msg="acquisition prune started" interval=24h0m0s` + "\n",
	} {
		if !strings.Contains(logged, want) {
			t.Errorf("want %q in\n%s", want, logged)
		}
	}
}

func emptyDatabasePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	admin := dbtest.Pool(t)
	name := pgx.Identifier{"app_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")}.Sanitize()
	if _, err := admin.Exec(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create an empty database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.Exec(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)"); err != nil {
			t.Errorf("drop the empty database: %v", err)
		}
	})
	poolCfg := admin.Config().Copy()
	poolCfg.ConnConfig.Database = strings.Trim(name, `"`)
	pool, err := pgxpool.NewWithConfig(context.Background(), poolCfg)
	if err != nil {
		t.Fatalf("connect to the empty database: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

var migrationStatementEnd = regexp.MustCompile(`;[ \t]*\r?\n`)

func migrationStatements(migration string) []string {
	var statements []string
	start := 0
	for _, end := range migrationStatementEnd.FindAllStringIndex(migration, -1) {
		if strings.Count(migration[start:end[0]], "$$")%2 == 1 {
			continue
		}
		statements = append(statements, migration[start:end[0]])
		start = end[1]
	}
	return append(statements, migration[start:])
}

func migratedDatabasePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := emptyDatabasePool(t)
	files, err := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("find the migrations: %v (found %d)", err, len(files))
	}
	conn, err := pool.Acquire(t.Context())
	if err != nil {
		t.Fatalf("acquire a connection: %v", err)
	}
	defer conn.Release()
	for _, file := range files {
		migration, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, statement := range migrationStatements(string(migration)) {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := conn.Conn().PgConn().Exec(t.Context(), statement).ReadAll(); err != nil {
				t.Fatalf("apply %s: %v", file, err)
			}
		}
	}
	return pool
}

func TestBackgroundJobs_ALeaderJobThatFailsLogsItsFailureUnderItsName(t *testing.T) {
	run := assembleOn(t, jwksConfig(t), emptyDatabasePool(t), refuseEgress{})
	const want = `level=WARN msg="deleted identity erasure failed" error=`

	if !timingtest.Within(time.Minute, func() bool { return strings.Contains(run.logs.String(), want) }) {
		t.Fatalf("no %q within a minute of leading a database with no tables:\n%s", want, run.logs.String())
	}
}
