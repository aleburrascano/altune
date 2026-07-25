package service

import "context"

type jobReporterKey struct{}

type jobReporter interface {
	meta(title, artist, album string)
	stage(name string)
	source(url string)
}

type noopJobReporter struct{}

func (noopJobReporter) meta(_, _, _ string) {}
func (noopJobReporter) stage(_ string)      {}
func (noopJobReporter) source(_ string)     {}

func withJobReporter(ctx context.Context, r jobReporter) context.Context {
	return context.WithValue(ctx, jobReporterKey{}, r)
}

func jobReporterFrom(ctx context.Context) jobReporter {
	if r, ok := ctx.Value(jobReporterKey{}).(jobReporter); ok && r != nil {
		return r
	}
	return noopJobReporter{}
}
