package core

import "time"

type Point struct {
	At    time.Time
	Value float64
}

type Series interface {
	Record(bucket, series string, p Point)
	Query(bucket, series string, from, to time.Time) ([]Point, error)
}

type SeriesWriter interface {
	UseSeries(s Series)
}
