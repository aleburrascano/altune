package main

import (
	"flag"
	"fmt"
	"io"
	"os"
)

type config struct {
	root     string
	baseline string
	write    bool
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func parseFlags(args []string, errOut io.Writer) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("moduleshape", flag.ContinueOnError)
	flags.SetOutput(errOut)
	flags.StringVar(&cfg.root, "root", ".", "go-api root to check")
	flags.StringVar(&cfg.baseline, "baseline", "module-shape.baseline", "baseline file")
	flags.BoolVar(&cfg.write, "write", false, "write the found violations to the baseline")
	return cfg, flags.Parse(args)
}

func run(args []string, out, errOut io.Writer) int {
	cfg, err := parseFlags(args, errOut)
	if err != nil {
		return 2
	}
	found, err := collect(cfg.root)
	if err != nil {
		_, _ = fmt.Fprintln(errOut, err)
		return 2
	}
	if cfg.write {
		return report(errOut, writeBaseline(cfg.baseline, found))
	}
	recorded, err := readBaseline(cfg.baseline)
	if err != nil {
		return report(errOut, err)
	}
	return compare(out, found, recorded)
}

func report(errOut io.Writer, err error) int {
	if err == nil {
		return 0
	}
	_, _ = fmt.Fprintln(errOut, err)
	return 2
}
