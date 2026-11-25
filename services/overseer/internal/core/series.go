package core

import "time"

type Point struct {
	At    time.Time
	Value float64
}

type Minute struct {
	At  time.Time
	Min float64
	Max float64
	Avg float64
}

type Series interface {
	Record(bucket, series string, p Point)
	Query(bucket, series string, from, to time.Time) ([]Point, error)
}

type DiscardSeries struct{}

func (DiscardSeries) Record(string, string, Point) {}

func (DiscardSeries) Query(string, string, time.Time, time.Time) ([]Point, error) {
	return nil, nil
}

type SeriesWriter interface {
	UseSeries(s Series)
}
