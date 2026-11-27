package main

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
)

func main() {
	switch cmd, rest := subcommand(os.Args); cmd {
	case "capture":
		os.Exit(runCapture(rest, os.Stdin, os.Stdout, clock.Now()))
	case "audit-qualifiers":
		os.Exit(runAuditQualifiers(os.Stdin, os.Stdout))
	case "live":
		os.Exit(runLiveCommand(rest, os.Stdout, os.Getenv))
	}

	var (
		goldenDir       = flag.String("goldens", "", "directory of golden json files (default: the embedded set)")
		baselinePath    = flag.String("baseline", "", "baselines.json to gate against; a regression exits non-zero")
		updateBaselines = flag.Bool("update-baselines", false, "overwrite the baseline file with this run's result")
		verbose         = flag.Bool("v", false, "keep pipeline logging instead of discarding it")
	)
	flag.Parse()

	if !*verbose {
		logging.Swap(slog.New(slog.NewTextHandler(io.Discard, nil)))
	}

	execute := func() error { return run(*goldenDir, *baselinePath) }
	if *updateBaselines {
		execute = func() error { return updateBaseline(*goldenDir, *baselinePath) }
	}
	if err := execute(); err != nil {
		fmt.Fprintf(os.Stderr, "acquisitioneval: %v\n", err)
		os.Exit(1)
	}
}

func run(goldenDir, baselinePath string) error {
	report, err := evaluate(goldenDir)
	if err != nil {
		return err
	}
	if baselinePath == "" {
		return nil
	}

	base, err := download.LoadEvalBaseline(baselinePath)
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
	return fmt.Errorf("%d regressions against %s", len(regressions), baselinePath)
}

func updateBaseline(goldenDir, baselinePath string) error {
	if baselinePath == "" {
		return fmt.Errorf("-update-baselines needs -baseline")
	}
	report, err := evaluate(goldenDir)
	if err != nil {
		return err
	}
	if err := report.WriteBaseline(baselinePath); err != nil {
		return err
	}
	fmt.Printf("baseline written to %s\n\n", baselinePath)
	return nil
}

func evaluate(goldenDir string) (download.EvalReport, error) {
	cases, err := loadCases(goldenDir)
	if err != nil {
		return download.EvalReport{}, err
	}
	if len(cases) == 0 {
		return download.EvalReport{}, fmt.Errorf("no golden cases found")
	}

	report := download.SummarizeEval(download.RunAllEvalCases(context.Background(), clock.Now, ids.Real, cases))
	fmt.Print(report.Render())
	return report, nil
}

func subcommand(args []string) (name string, rest []string) {
	if len(args) > 1 && (args[1] == "capture" || args[1] == "audit-qualifiers" || args[1] == "live") {
		return args[1], args[2:]
	}
	return "", args[1:]
}

func loadCases(goldenDir string) ([]download.EvalCase, error) {
	if goldenDir == "" {
		return download.LoadEmbeddedEvalCases()
	}
	return download.LoadEvalDir(goldenDir)
}
