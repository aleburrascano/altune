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
	Total                  int           `json:"total"`
	Passed                 int           `json:"passed"`
	Classes                []ClassResult `json:"classes"`
	Failures               []Outcome     `json:"-"`
	Pending                []Outcome     `json:"-"`
	MedianSimulatedSeconds float64       `json:"median_simulated_seconds"`
	MeanAttempts           float64       `json:"mean_attempts"`
}

func (r Report) Accuracy() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Passed) / float64(r.Total)
}

func Summarize(outcomes []Outcome) Report {
	byClass := make(map[string]*ClassResult)
	var report Report
	var seconds []float64
	var attempts []int

	for _, o := range outcomes {
		if o.Pending {
			report.Pending = append(report.Pending, o)
			continue
		}
		report.Total++
		seconds = append(seconds, o.SimulatedSeconds)
		attempts = append(attempts, o.Attempts)
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
	report.MedianSimulatedSeconds = medianOf(seconds)
	report.MeanAttempts = meanOf(attempts)
	return report
}

func medianOf(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]float64(nil), values...)
	sort.Float64s(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

func meanOf(values []int) float64 {
	if len(values) == 0 {
		return 0
	}
	sum := 0
	for _, v := range values {
		sum += v
	}
	return float64(sum) / float64(len(values))
}

func (r Report) Render() string {
	var b strings.Builder

	fmt.Fprintf(&b, "\nAcquisition selection eval — %d/%d (%.1f%%) · median %.1fs · mean %.1f attempts\n\n",
		r.Passed, r.Total, r.Accuracy()*100, r.MedianSimulatedSeconds, r.MeanAttempts)
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
	renderPending(&b, r.Pending)
	b.WriteString("\n")
	return b.String()
}

func renderPending(b *strings.Builder, pending []Outcome) {
	if len(pending) == 0 {
		return
	}
	fmt.Fprintf(b, "\n  Pending (not scored, owned by a later ticket):\n")
	for _, p := range pending {
		fmt.Fprintf(b, "    [%s] %s %s\n        owner: %s\n        %s\n",
			p.Case.Class, pendingVerdict(p), p.Case.ID, p.Case.Pending, p.Reason)
	}
}

func pendingVerdict(p Outcome) string {
	if p.Pass {
		return "PASS"
	}
	return "FAIL"
}

type Baseline struct {
	Accuracy      float64            `json:"accuracy"`
	Classes       map[string]float64 `json:"classes"`
	MedianSeconds float64            `json:"median_seconds,omitempty"`
	MeanAttempts  float64            `json:"mean_attempts,omitempty"`
}

const (
	baselineMargin  = 0.01
	timeMarginRatio = 1.05
)

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
	b := Baseline{
		Accuracy:      r.Accuracy(),
		Classes:       make(map[string]float64, len(r.Classes)),
		MedianSeconds: r.MedianSimulatedSeconds,
		MeanAttempts:  r.MeanAttempts,
	}
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
	if base.MedianSeconds > 0 && r.MedianSimulatedSeconds > base.MedianSeconds*timeMarginRatio {
		out = append(out, fmt.Sprintf("median simulated seconds %.1f above baseline %.1f",
			r.MedianSimulatedSeconds, base.MedianSeconds))
	}
	if base.MeanAttempts > 0 && r.MeanAttempts > base.MeanAttempts*timeMarginRatio {
		out = append(out, fmt.Sprintf("mean attempts %.2f above baseline %.2f",
			r.MeanAttempts, base.MeanAttempts))
	}
	sort.Strings(out)
	return out
}
