package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	discoveryEval "altune/go-api/internal/discovery/service/eval"

	"github.com/jackc/pgx/v5/pgxpool"
)

type metricsFile struct {
	Mode        string                      `json:"mode"`
	GeneratedAt string                      `json:"generated_at"`
	Metrics     []discoveryEval.NamedMetric `json:"metrics"`
}

func maybeWriteMetrics(path, mode string, metrics []discoveryEval.NamedMetric) error {
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(metricsFile{
		Mode:        mode,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Metrics:     metrics,
	}, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal metrics: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write metrics %s: %w", path, err)
	}
	return nil
}

func loadMetricsFiles(dir string) ([]metricsFile, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "metrics-*.json"))
	if err != nil {
		return nil, fmt.Errorf("glob metrics in %s: %w", dir, err)
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no metrics-*.json found in %s", dir)
	}
	sort.Strings(paths)

	out := make([]metricsFile, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", p, err)
		}
		var mf metricsFile
		if err := json.Unmarshal(data, &mf); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p, err)
		}
		out = append(out, mf)
	}
	return out, nil
}

func runReport(ctx context.Context, pool *pgxpool.Pool, opts options) error {
	if opts.reportsDir == "" {
		return fmt.Errorf("report mode needs -reports pointing at a directory of metrics-*.json")
	}

	files, err := loadMetricsFiles(opts.reportsDir)
	if err != nil {
		return err
	}
	baselines, err := loadBaselines(opts.baselinesPath)
	if err != nil {
		return err
	}

	values := map[string]float64{}
	gatesByMode := map[string][]discoveryEval.GateResult{}
	modes := make([]string, 0, len(files))
	regressed := []discoveryEval.GateResult{}

	for _, f := range files {
		modes = append(modes, f.Mode)
		gates := baselines.GateAll(f.Metrics)
		gatesByMode[f.Mode] = gates
		for _, g := range gates {
			values[g.Metric] = g.Current
			if g.Regressed {
				regressed = append(regressed, g)
			}
		}
	}

	fmt.Print(renderReport(modes, gatesByMode, regressed))

	if err := discoveryPersistence.NewPgxMetricsRollup(pool).
		RecordMetrics(ctx, time.Now().UTC(), values); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "recorded %d metric(s) to discovery_metrics\n", len(values))

	if len(regressed) > 0 {
		digestPath := filepath.Join(opts.reportsDir, "regressions.txt")
		if err := os.WriteFile(digestPath, []byte(regressionDigest(regressed)), 0o644); err != nil {
			return fmt.Errorf("write regression digest: %w", err)
		}
		return errRegressed
	}
	return nil
}

func renderReport(modes []string, gatesByMode map[string][]discoveryEval.GateResult, regressed []discoveryEval.GateResult) string {
	var b strings.Builder

	fmt.Fprintf(&b, "# Discovery eval nightly — %s\n\n", time.Now().UTC().Format("2006-01-02"))
	if len(regressed) == 0 {
		fmt.Fprintf(&b, "**No regressions** across %d mode(s).\n\n", len(modes))
	} else {
		fmt.Fprintf(&b, "**%d metric(s) regressed** across %d mode(s):\n\n", len(regressed), len(modes))
		for _, g := range regressed {
			fmt.Fprintf(&b, "- `%s` %.4f vs baseline %.4f ±%.4f (%+.4f)\n", g.Metric, g.Current, g.Baseline, g.Margin, g.Current-g.Baseline)
		}
		b.WriteString("\n")
	}

	b.WriteString("| Mode | Metric | Current | Baseline | Margin | Delta | Status |\n")
	b.WriteString("|---|---|---:|---:|---:|---:|---|\n")
	for _, mode := range modes {
		for _, g := range discoveryEval.SortedGates(gatesByMode[mode]) {
			if g.Missing {
				fmt.Fprintf(&b, "| %s | `%s` | %.4f | — | — | — | new |\n", mode, g.Metric, g.Current)
				continue
			}
			status := "ok"
			if g.Regressed {
				status = "**REGRESSED**"
			}
			fmt.Fprintf(&b, "| %s | `%s` | %.4f | %.4f | ±%.4f | %+.4f | %s |\n",
				mode, g.Metric, g.Current, g.Baseline, g.Margin, g.Current-g.Baseline, status)
		}
	}
	b.WriteString("\n")
	return b.String()
}

func regressionDigest(gates []discoveryEval.GateResult) string {
	names := make([]string, 0, len(gates))
	for _, g := range gates {
		names = append(names, fmt.Sprintf("%s %+.4f", g.Metric, g.Current-g.Baseline))
	}
	return strings.Join(names, ", ")
}
