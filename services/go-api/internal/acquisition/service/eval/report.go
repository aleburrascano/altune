package eval

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

type ClassResult struct {
	Class  string `json:"class"`
	Total  int    `json:"total"`
	Passed int    `json:"passed"`
}

func (c ClassResult) Accuracy() float64 {
	if c.Total == 0 {
		return 0
	}
	return float64(c.Passed) / float64(c.Total)
}

type Report struct {
	Total    int           `json:"total"`
	Passed   int           `json:"passed"`
	Classes  []ClassResult `json:"classes"`
	Failures []Outcome     `json:"-"`
}

func (r Report) Accuracy() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Total)
}

func Summarize(outcomes []Outcome) Report {
	byClass := make(map[string]*ClassResult)
	report := Report{Total: len(outcomes)}

	for _, o := range outcomes {
		cr, ok := byClass[o.Case.Class]
		if !ok {
			cr = &ClassResult{Class: o.Case.Class}
			byClass[o.Case.Class] = cr
		}
		cr.Total++
		if o.Pass {
			cr.Passed++
			report.Passed++
			continue
		}
		report.Failures = append(report.Failures, o)
	}

	for _, cr := range byClass {
		report.Classes = append(report.Classes, *cr)
	}
	sort.SliceStable(report.Classes, func(i, j int) bool {
		return report.Classes[i].Class < report.Classes[j].Class
	})
	return report
}

func (r Report) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "\nAcquisition selection eval — %d/%d (%.1f%%)\n\n",
		r.Passed, r.Total, r.Accuracy()*100)
	fmt.Fprintf(&b, "  %-8s %-8s %s\n", "CLASS", "SCORE", "ACCURACY")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("-", 34))
	for _, c := range r.Classes {
		fmt.Fprintf(&b, "  %-8s %-8s %.0f%%\n",
			c.Class, fmt.Sprintf("%d/%d", c.Passed, c.Total), c.Accuracy()*100)
	}

	if len(r.Failures) > 0 {
		fmt.Fprintf(&b, "\n  Failures:\n")
		for _, f := range r.Failures {
			fmt.Fprintf(&b, "    [%s] %s\n        %s\n", f.Case.Class, f.Case.ID, f.Reason)
			if f.Case.Note != "" {
				fmt.Fprintf(&b, "        note: %s\n", f.Case.Note)
			}
		}
	}
	b.WriteString("\n")
	return b.String()
}

type Baseline struct {
	Accuracy float64            `json:"accuracy"`
	Classes  map[string]float64 `json:"classes"`
}

const baselineMargin = 0.01

func LoadBaseline(path string) (Baseline, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Baseline{}, fmt.Errorf("read baseline: %w", err)
	}
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return Baseline{}, fmt.Errorf("parse baseline: %w", err)
	}
	return b, nil
}

func (r Report) WriteBaseline(path string) error {
	b := Baseline{Accuracy: r.Accuracy(), Classes: make(map[string]float64, len(r.Classes))}
	for _, c := range r.Classes {
		b.Classes[c.Class] = c.Accuracy()
	}
	raw, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func (r Report) Regressions(base Baseline) []string {
	var out []string
	if r.Accuracy() < base.Accuracy-baselineMargin {
		out = append(out, fmt.Sprintf("overall accuracy %.1f%% below baseline %.1f%%",
			r.Accuracy()*100, base.Accuracy*100))
	}
	for _, c := range r.Classes {
		want, ok := base.Classes[c.Class]
		if !ok {
			continue
		}
		if c.Accuracy() < want-baselineMargin {
			out = append(out, fmt.Sprintf("class %s accuracy %.0f%% below baseline %.0f%%",
				c.Class, c.Accuracy()*100, want*100))
		}
	}
	sort.Strings(out)
	return out
}
