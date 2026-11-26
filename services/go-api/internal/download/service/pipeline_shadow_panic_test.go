package service_test

import (
	"altune/go-api/internal/download/internal/adapters/scratchfiles"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

type panicOnShadowHandler struct {
	slog.Handler
}

func (h panicOnShadowHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "acquisition.judge_shadow" {
		panic("shadow log blew up")
	}
	if err := h.Handler.Handle(ctx, r); err != nil {
		return fmt.Errorf("handle the log record: %w", err)
	}
	return nil
}

func TestRunPipeline_APanicInsideTheShadowJudgeIsLoggedAndTheRunStillSucceeds(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(panicOnShadowHandler{slog.NewTextHandler(&logs, nil)}))
	t.Cleanup(func() { slog.SetDefault(prev) })
	workspace := scratchfiles.New()
	source := finesseSource{candidates: []ports.AudioCandidate{finesseOn(finesseAlbumURL, "24K Magic")}}
	steps := service.CoreSteps(service.NewSourceRegistry(source), nil, &objectStore{objects: map[string]string{}}, nil, nil, workspace, nil)
	ac := &service.AcquisitionContext{Track: service.TrackRef{
		ID: "track-1", UserID: "u1", Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic", CatalogDuration: 191,
	}}

	err := service.RunPipeline(context.Background(), steps, ac)

	service.CleanupTemp(context.Background(), workspace, ac)
	if err != nil {
		t.Fatalf("RunPipeline: %v, want the shadow judge's panic contained", err)
	}
	for _, want := range []string{"acquisition.judge_shadow_panic", "track_id=track-1", `panic="shadow log blew up"`} {
		if !strings.Contains(logs.String(), want) {
			t.Errorf("log = %q, want %s", logs.String(), want)
		}
	}
}
