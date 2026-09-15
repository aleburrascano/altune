package persistence

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// countingPool is a pgxPool that answers QueryRow with a fixed count (or error)
// and records every SQL it was asked to run. Only QueryRow is reachable from
// pageTotal; the rest fail loudly if a change starts calling them.
type countingPool struct {
	count   int
	err     error
	queries []string
}

func (p *countingPool) Begin(context.Context) (pgx.Tx, error) {
	return nil, errors.New("unexpected Begin")
}

func (p *countingPool) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unexpected Query")
}

func (p *countingPool) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("unexpected Exec")
}

func (p *countingPool) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	p.queries = append(p.queries, sql)
	return countRow{n: p.count, err: p.err}
}

type countRow struct {
	n   int
	err error
}

func (r countRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for _, d := range dest {
		ptr, ok := d.(*int)
		if !ok || ptr == nil {
			return fmt.Errorf("dest %T, want *int", d)
		}
		*ptr = r.n
	}
	return nil
}

// TestPageTotal_SkipsCountOnlyWhenPageProvesTotal pins the count strategy that
// replaced COUNT(*) OVER (): a short page is the last page, so its total is
// derived without a second query; any page that cannot prove the total runs one
// count(*), clamped so a page is never larger than its reported total.
func TestPageTotal_SkipsCountOnlyWhenPageProvesTotal(t *testing.T) {
	cases := []struct {
		name          string
		limit, offset int
		got, dbCount  int
		want          int
		wantCountSQL  bool
	}{
		{"short first page is the whole set", 50, 0, 7, 999, 7, false},
		{"short later page ends the set", 50, 100, 20, 999, 120, false},
		{"empty first page means zero", 50, 0, 0, 999, 0, false},
		{"full page needs a count", 50, 0, 50, 180, 180, true},
		{"empty page past offset needs a count", 50, 500, 0, 180, 180, true},
		{"count behind a concurrent add is clamped", 50, 50, 50, 90, 100, true},
	}
	for _, c := range cases {
		pool := &countingPool{count: c.dbCount}
		total, err := pageTotal(context.Background(), pool, c.limit, c.offset, c.got,
			`SELECT count(*) FROM tracks WHERE user_id = $1`, uuid.New())
		if err != nil {
			t.Fatalf("%s: pageTotal error = %v", c.name, err)
		}
		if total != c.want {
			t.Errorf("%s: total = %d, want %d", c.name, total, c.want)
		}
		if ran := len(pool.queries) == 1; ran != c.wantCountSQL {
			t.Errorf("%s: count query ran = %v (queries %v), want %v", c.name, ran, pool.queries, c.wantCountSQL)
		}
	}
}

func TestPageTotal_CountErrorPropagates(t *testing.T) {
	boom := errors.New("boom")
	pool := &countingPool{err: boom}
	_, err := pageTotal(context.Background(), pool, 10, 0, 10, `SELECT count(*) FROM tracks WHERE user_id = $1`, uuid.New())
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want wrapping %v", err, boom)
	}
}

// TestPgxTrackRepo_ListFilteredForUser_TotalIsExactOnEveryPage runs the real
// page + count SQL against Postgres: totals must match the filtered set on a
// full page (count path), a short page (derived path), and a page past the end.
// The old COUNT(*) OVER () reported 0 past the end because no row carried it.
func TestPgxTrackRepo_ListFilteredForUser_TotalIsExactOnEveryPage(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxCatalogTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})

	base := time.Now().UTC().Truncate(time.Second)
	for i, title := range []string{"Moon One", "Sun Two", "Moon Three", "Sun Four", "Moon Five"} {
		seedLibraryTrack(t, repo, userId, libraryTrackSpec{
			title: title, artist: "Artist", album: "Album",
			addedAt: base.Add(time.Duration(-i) * time.Minute),
		})
	}

	cases := []struct {
		name          string
		search        string
		limit, offset int
		wantLen       int
		wantTotal     int
	}{
		{"unfiltered full page", "", 2, 0, 2, 5},
		{"unfiltered short last page", "", 2, 4, 1, 5},
		{"unfiltered past the end", "", 2, 10, 0, 5},
		{"search full page", "moon", 2, 0, 2, 3},
		{"search short last page", "moon", 2, 2, 1, 3},
		{"search past the end", "moon", 2, 10, 0, 3},
		{"search exact page boundary", "moon", 3, 0, 3, 3},
		{"search no match", "nothing", 2, 0, 0, 0},
	}
	for _, c := range cases {
		got, total, err := repo.ListFilteredForUser(ctx, userId, domain.LibraryQuery{
			Search: c.search, Sort: domain.SortRecent, Limit: c.limit, Offset: c.offset,
		})
		if err != nil {
			t.Fatalf("%s: ListFilteredForUser: %v", c.name, err)
		}
		if len(got) != c.wantLen || total != c.wantTotal {
			t.Errorf("%s: len=%d total=%d, want len=%d total=%d", c.name, len(got), total, c.wantLen, c.wantTotal)
		}
	}
}

func TestPgxTrackRepo_ListForUser_TotalPastTheEnd(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxTrackRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE user_id = $1`, userId.UUID())
	})
	for i := 0; i < 3; i++ {
		tr := newTestTrackForDB(t, userId)
		if _, _, err := repo.Add(ctx, tr); err != nil {
			t.Fatalf("Add: %v", err)
		}
	}

	got, total, err := repo.ListForUser(ctx, userId, 2, 10)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(got) != 0 || total != 3 {
		t.Fatalf("len=%d total=%d, want len=0 total=3", len(got), total)
	}
}
