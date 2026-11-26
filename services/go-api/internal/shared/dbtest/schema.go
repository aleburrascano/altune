package dbtest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	schemaPrefix       = "testdb_"
	staleSchemaAge     = 6 * time.Hour
	schemaSetupTimeout = 2 * time.Minute
	grantsMigration    = "052_app_role_grants.sql"
)

var (
	schemaOnce sync.Once
	schemaURL  string
	errSchema  error
)

func packageSchemaURL(base string) (string, error) {
	schemaOnce.Do(func() {
		schemaURL, errSchema = createPackageSchema(base)
	})
	return schemaURL, errSchema
}

func createPackageSchema(base string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), schemaSetupTimeout)
	defer cancel()

	conn, err := pgx.Connect(ctx, base)
	if err != nil {
		return "", fmt.Errorf("connect to create the test schema: %w", err)
	}
	defer func() { _ = conn.Close(context.Background()) }()

	if err := dropStaleSchemas(ctx, conn); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s%d_%d", schemaPrefix, time.Now().Unix(), os.Getpid())
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := conn.Exec(ctx, "CREATE SCHEMA "+quoted); err != nil {
		return "", fmt.Errorf("create schema %s: %w", name, err)
	}
	if _, err := conn.Exec(ctx, "SET search_path TO "+quoted+", public"); err != nil {
		return "", fmt.Errorf("select schema %s: %w", name, err)
	}
	if err := applyMigrations(ctx, conn); err != nil {
		return "", fmt.Errorf("migrate schema %s: %w", name, err)
	}
	return withSearchPath(base, name)
}

func withSearchPath(base, schema string) (string, error) {
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("parse DATABASE_URL: %w", err)
	}
	query := parsed.Query()
	query.Set("search_path", schema+",public")
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func applyMigrations(ctx context.Context, conn *pgx.Conn) error {
	dir, err := migrationsDir()
	if err != nil {
		return err
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(files)
	for _, file := range files {
		if filepath.Base(file) == grantsMigration {
			continue
		}
		raw, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read %s: %w", filepath.Base(file), err)
		}
		inline := strings.ReplaceAll(string(raw), "CONCURRENTLY ", "")
		if _, err := conn.Exec(ctx, inline); err != nil {
			return fmt.Errorf("apply %s: %w", filepath.Base(file), err)
		}
	}
	return nil
}

func migrationsDir() (string, error) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("locate the migrations next to the dbtest package")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations"), nil
}

func dropStaleSchemas(ctx context.Context, conn *pgx.Conn) error {
	rows, err := conn.Query(ctx, `SELECT nspname FROM pg_namespace WHERE nspname LIKE $1`, schemaPrefix+"%")
	if err != nil {
		return fmt.Errorf("list test schemas: %w", err)
	}
	var stale []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return fmt.Errorf("scan test schema: %w", err)
		}
		if schemaIsStale(name, time.Now()) {
			stale = append(stale, name)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("list test schemas: %w", err)
	}
	for _, name := range stale {
		if _, err := conn.Exec(ctx, "DROP SCHEMA IF EXISTS "+pgx.Identifier{name}.Sanitize()+" CASCADE"); err != nil {
			return fmt.Errorf("drop stale schema %s: %w", name, err)
		}
	}
	return nil
}

func schemaIsStale(name string, now time.Time) bool {
	stamp, _, found := strings.Cut(strings.TrimPrefix(name, schemaPrefix), "_")
	if !found {
		return false
	}
	created, err := strconv.ParseInt(stamp, 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(created, 0)) > staleSchemaAge
}

func requireSchemaURL(t testing.TB, base string) string {
	t.Helper()
	scoped, err := packageSchemaURL(base)
	if err != nil {
		t.Fatalf("prepare this package's test schema: %v", err)
	}
	return scoped
}
