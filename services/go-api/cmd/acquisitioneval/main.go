package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"

	"altune/go-api/internal/acquisition/service/eval"
)

func main() {
	var (
		goldenDir      = flag.String("goldens", "", "directory of golden json files (default: the embedded set)")
		baselinePath   = flag.String("baseline", "", "baselines.json to gate against; a regression exits non-zero")
		updateBaseline = flag.Bool("update-baselines", false, "overwrite the baseline file with this run's result")
		verbose        = flag.Bool("v", false, "keep pipeline logging instead of discarding it")
	)
	flag.Parse()

	if !*verbose {
		slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	if err := run(*goldenDir, *baselinePath, *updateBaseline); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: %v\n", err)
		os.Exit(1)
	}
}

func run(goldenDir, baselinePath string, updateBaseline bool) error {
	cases, err := loadCases(goldenDir)
	if err != nil {
		return err
	}
	if len(cases) == 0 {
		return fmt.Errorf("no golden cases found")
	}

	report := eval.Summarize(eval.RunAll(context.Background(), cases))
	fmt.Print(report.Render())

	if updateBaseline {
		if baselinePath == "" {
			return fmt.Errorf("-update-baselines needs -baseline")
		}
		if err := report.WriteBaseline(baselinePath); err != nil {
			return err
		}
		fmt.Printf("baseline written to %s\n\n", baselinePath)
		return nil
	}

	if baselinePath == "" {
		return nil
	}

	base, err := eval.LoadBaseline(baselinePath)
	if err != nil {
		return err
	}
	regressions := report.Regressions(base)
	if len(regressions) == 0 {
		fmt.Printf("no regression against %s\n\n", baselinePath)
		return nil
	}
	for _, r := range regressions {
		fmt.Fprintf(os.Stderr, "REGRESSION: %s\n", r)
	}
	os.Exit(1)
	return nil
}

func loadCases(goldenDir string) ([]eval.Case, error) {
	if goldenDir == "" {
		return eval.LoadEmbedded()
	}
	return eval.LoadDir(goldenDir)
}
