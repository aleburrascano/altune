package guard_test

import (
	"altune/overseer/internal/authn"
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/shell"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// updateFixtures regenerates web/src/test/fixtures/*.json from the current Go
// wire JSON instead of comparing against them. Run it once, by hand, whenever a
// snapshot/series/health field is added, renamed or removed on purpose:
//
//	go test ./internal/guard/... -run TestContract -update
//
// then re-run without -update to confirm it is green, and update
// web/src/test/types.ts (and the TS contract test) to match if the shape moved.
var updateFixtures = flag.Bool("update", false, "rewrite the contract fixtures from the current Go wire JSON")

const (
	contractOwnerID = "contract-owner"
	contractToken   = "contract-owner-token"
)

var contractTime = time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

// contractVerifier is a fixed shell.Verifier: it accepts only contractToken, so
// the contract server can be driven with the owner-only guard in place rather
// than bypassed, matching what production actually serves.
type contractVerifier struct{}

func (contractVerifier) Verify(_ context.Context, token string) (authn.Claims, error) {
	if token != contractToken {
		return authn.Claims{}, errors.New("invalid token")
	}
	return authn.Claims{Subject: contractOwnerID}, nil
}

// contractBucket is a representative bucket: source_down with a classified
// reason and a real headline/severity, so the snapshot fixture exercises every
// optional field (reason, spark) the plain-happy-path snapshot would omit.
type contractBucket struct{}

func (contractBucket) Meta() core.Meta                                { return core.Meta{ID: "reliability", Title: "Reliability"} }
func (contractBucket) Collect(context.Context) ([]core.Signal, error) { return nil, nil }
func (contractBucket) Store([]core.Signal)                            {}
func (contractBucket) KeySeries() string                              { return "latency_ms" }

func (contractBucket) Snapshot() core.Snapshot {
	return core.Snapshot{
		ID:        "reliability",
		Title:     "Reliability",
		State:     core.StateSourceDown,
		Severity:  core.SeverityWarn,
		Headline:  "p95 latency 240ms",
		Reason:    goapi.ReasonDegraded,
		UpdatedAt: contractTime,
		Data:      json.RawMessage(`{"p50":80,"p95":240,"p99":410}`),
	}
}

// contractRegistry serves the one contractBucket.
type contractRegistry struct{ bucket core.Bucket }

func (r contractRegistry) Buckets() []core.Bucket { return []core.Bucket{r.bucket} }

func (r contractRegistry) Get(id string) (core.Bucket, bool) {
	if id == r.bucket.Meta().ID {
		return r.bucket, true
	}
	return nil, false
}

// contractSeries answers both the raw-point and minute-rollup reads, so the
// series fixture is pinned once with the min/max fields a 7d range actually
// carries, not a raw point that would leave them absent.
type contractSeries struct{}

func (contractSeries) Names(string) ([]string, error) { return []string{"latency_ms"}, nil }

func (contractSeries) Query(_, _ string, _, _ time.Time) ([]core.Point, error) {
	return []core.Point{{At: contractTime, Value: 240}}, nil
}

func (contractSeries) Minutes(_, _ string, _, _ time.Time) ([]core.Minute, error) {
	return []core.Minute{{At: contractTime, Min: 80, Max: 410, Avg: 240}}, nil
}

// contractServer wires a shell.Handler the way production does — owner-only
// guard included — around the fixed bucket, series and health seams above, so
// every fixture below is captured from the real HTTP surface, not a hand-built
// struct that could drift from what handlers actually emit.
func contractServer() http.Handler {
	return shell.NewHandler(
		contractRegistry{bucket: contractBucket{}},
		shell.WithVerifier(contractVerifier{}),
		shell.WithOwnerUserID(contractOwnerID),
		shell.WithSeries(contractSeries{}),
		shell.WithCollectStatus(func() shell.CollectStatus {
			return shell.CollectStatus{Healthy: true, LastCycle: contractTime, OK: 8, Failed: 1}
		}),
		shell.WithCredentialHealth(func() goapi.CredentialHealth {
			return goapi.CredentialHealth{
				LastRefresh:         contractTime,
				ConsecutiveFailures: 0,
				PersistFailed:       false,
				PasswordGrant:       true,
			}
		}),
	).Router()
}

func contractGet(t *testing.T, srv http.Handler, path string) []byte {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+contractToken)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s = %d, want 200 (%s)", path, rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

const fixtureDir = "../../web/src/test/fixtures"

// pinFixture is the golden-file half of the contract: it compares the given
// wire bytes, pretty-printed, against web/src/test/fixtures/<name>, so a Go
// json tag renamed or removed changes the bytes and fails this test — the fix
// is either to restore the tag or to run with -update and carry the shape
// change into web/src/types.ts on purpose.
func pinFixture(t *testing.T, name string, wire []byte) {
	t.Helper()
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, wire, "", "  "); err != nil {
		t.Fatalf("wire JSON for %s does not parse: %v (%s)", name, err, wire)
	}
	pretty.WriteByte('\n')
	path := fixtureDir + "/" + name

	if *updateFixtures {
		if err := os.WriteFile(path, pretty.Bytes(), 0o644); err != nil {
			t.Fatalf("write fixture %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v (generate it with: go test ./internal/guard/... -run TestContract -update)", path, err)
	}
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Errorf("%s has drifted from the Go wire JSON.\n got:\n%s\nwant:\n%s\n(if this is intentional, run: go test ./internal/guard/... -run TestContract -update, then update web/src/types.ts to match)",
			path, pretty.String(), want)
	}
}

func TestContractSnapshotFixtureMatchesTheBucketsWireJSON(t *testing.T) {
	srv := contractServer()
	body := contractGet(t, srv, "/api/buckets")

	var envelope struct {
		Buckets []json.RawMessage `json:"buckets"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode /api/buckets: %v (%s)", err, body)
	}
	if len(envelope.Buckets) != 1 {
		t.Fatalf("buckets = %d, want 1", len(envelope.Buckets))
	}
	pinFixture(t, "snapshot.json", envelope.Buckets[0])
}

func TestContractSeriesFixtureMatchesTheSeriesWireJSON(t *testing.T) {
	srv := contractServer()
	// 7d is the only range that carries min/max (minute rollups); pinning that
	// range is what proves the fixture (and the TS types) cover those fields.
	body := contractGet(t, srv, "/api/buckets/reliability/series?range=7d")
	pinFixture(t, "series.json", body)
}

func TestContractHealthFixtureMatchesTheHealthWireJSON(t *testing.T) {
	srv := contractServer()
	body := contractGet(t, srv, "/api/health")
	pinFixture(t, "health.json", body)
}
