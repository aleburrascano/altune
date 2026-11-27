package core

import (
	"fmt"
	"sync"
	"time"
)

const (
	SLOWindowDays = 28
	secondsPerDay = 24 * 60 * 60
)

type SLO struct {
	Journey string
	Name    string
	Target  float64

	MinTotal uint64
}

type SLOStatus struct {
	Journey         string  `json:"journey"`
	Name            string  `json:"name"`
	Target          float64 `json:"target"`
	WindowDays      int     `json:"windowDays"`
	Good            uint64  `json:"good"`
	Total           uint64  `json:"total"`
	Attainment      float64 `json:"attainment"`
	Measured        bool    `json:"measured"`
	Met             bool    `json:"met"`
	BudgetRemaining float64 `json:"budgetRemaining"`
}

type dayTally struct {
	day   int64
	good  uint64
	total uint64
}

type SLOWindow struct {
	slo SLO

	mu   sync.Mutex
	days [SLOWindowDays]dayTally
}

func NewSLOWindow(slo SLO) *SLOWindow {
	if slo.Target <= 0 || slo.Target >= 1 {
		panic(fmt.Sprintf("core.NewSLOWindow: the target of %q must be between 0 and 1 exclusive, got %v", slo.Name, slo.Target))
	}
	return &SLOWindow{slo: slo}
}

func (w *SLOWindow) Record(at time.Time, good, total uint64) {
	if total == 0 {
		return
	}
	good = min(good, total)
	day := dayOf(at)
	w.mu.Lock()
	defer w.mu.Unlock()
	slot := &w.days[day%SLOWindowDays]
	if slot.day != day {
		*slot = dayTally{day: day}
	}
	slot.good += good
	slot.total += total
}

func (w *SLOWindow) Status(now time.Time) SLOStatus {
	today := dayOf(now)
	var good, total uint64
	w.mu.Lock()
	for i := range w.days {
		d := &w.days[i]
		if d.total > 0 && d.day > today-SLOWindowDays && d.day <= today {
			good += d.good
			total += d.total
		}
	}
	w.mu.Unlock()

	status := SLOStatus{
		Journey:         w.slo.Journey,
		Name:            w.slo.Name,
		Target:          w.slo.Target,
		WindowDays:      SLOWindowDays,
		Good:            good,
		Total:           total,
		BudgetRemaining: 1,
	}
	if total == 0 || total < w.slo.MinTotal {
		return status
	}
	status.Measured = true
	status.Attainment = float64(good) / float64(total)
	status.Met = status.Attainment >= w.slo.Target
	allowedBad := float64(total) * (1 - w.slo.Target)
	status.BudgetRemaining = 1 - float64(total-good)/allowedBad
	return status
}

func dayOf(at time.Time) int64 {
	return at.UTC().Unix() / secondsPerDay
}
