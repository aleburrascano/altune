package main

import (
	"altune/go-api/internal/discovery"
	"altune/go-api/internal/shared/config"
	"context"
	_ "embed"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

const goldenConcurrency = 1

//go:embed testdata/identity_goldens.json
var identityGoldensJSON []byte

func loadGoldens() ([]discovery.GoldenCase, error) {
	return discovery.ParseGoldenCases(identityGoldensJSON)
}

func runGolden(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool, redisClient *goredis.Client, opts options) error {
	cases, err := loadGoldens()
	if err != nil {
		return err
	}
	searcher, drain := buildEvalSearcher(ctx, cfg, pool, redisClient)
	return runGoldenWith(ctx, cases, searcher, drain, opts)
}

func runGoldenWith(ctx context.Context, cases []discovery.GoldenCase, searcher discovery.Searcher, drain func(), opts options) error {
	var last discovery.GoldenReport
	once := func() (discovery.HarnessReport, error) {
		fmt.Fprintf(os.Stderr, "golden eval over %d identity cases (sequential, so provider timeouts do not flip cases)...\n", len(cases))
		last = discovery.RunGoldenEval(ctx, cases, searcher, goldenConcurrency)
		drain()
		return last, nil
	}
	human := func(r discovery.HarnessReport) string { return renderGolden(r.(discovery.GoldenReport)) }
	if err := runHarness("golden", once, human, opts); err != nil {
		return err
	}
	if !opts.updateBaselines && last.Unexpected() > 0 {
		return errRegressed
	}
	return nil
}

func renderGolden(report discovery.GoldenReport) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# Discovery golden eval: %d identity cases\n\n", len(report.Results))
	fmt.Fprintf(&b, "  hits:                      %d (top-1: %d)\n", report.Hits(), report.Top1Hits())
	fmt.Fprintf(&b, "  known failing:             %d\n", report.KnownFailing())
	fmt.Fprintf(&b, "  unexpected (fail the run): %d\n\n", report.Unexpected())
	for _, res := range report.Results {
		if res.Verdict == discovery.GoldenPass {
			continue
		}
		fmt.Fprintf(&b, "  %-42s %-7s %-30s %-6s pos=%d", res.Case.Query, res.Case.Kind, res.Verdict, res.Case.KnownFailing, res.Position)
		if res.Top != nil {
			fmt.Fprintf(&b, "  top=%s %q by %q", res.Top.Kind, res.Top.Title, res.Top.Subtitle)
		}
		if res.Error != "" {
			fmt.Fprintf(&b, "  error=%s", res.Error)
		}
		b.WriteString("\n")
	}
	return b.String()
}
