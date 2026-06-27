package service

import "context"

// jobReporterKey is an unexported context key for the in-flight acquisition job
// reporter (per go-context: context keys must be unexported types).
type jobReporterKey struct{}

// jobReporter receives live updates about an in-flight acquisition job as it
// progresses through the pipeline. The scheduler implements it (updating its
// in-memory job record); the acquire service and RunPipeline call it. Always
// resolved through jobReporterFrom, which returns a no-op when none is wired, so
// the eval/test paths that call Execute without a scheduler are unaffected.
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
