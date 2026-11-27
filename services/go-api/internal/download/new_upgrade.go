package download

import (
	"altune/go-api/internal/download/internal/adapters/previewcheck"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/download/service/replacement"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
)

func buildUpgradeLane(d Deps, replace *service.ReplaceService, checker *service.InPlaceChecker) service.UpgradeLane {
	cfg := d.Config
	if cfg.AcquisitionUpgradeConcurrency < 1 {
		return service.NoUpgradeLane()
	}
	store := d.Database.UpgradeStore()
	inPlace, err := replacement.InPlaceCheckOf(checker, d.InPlaceRecorder(store), cfg.AcquisitionUpgradeVerifyPerHour, clock.Now, clock.Sleep)
	if err != nil {
		panic(err)
	}
	runner := replacement.MustNewRunner(d.Ledger, replace, store,
		replacement.Config{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}, inPlace)
	return service.MustUpgradeLaneOf(runner, cfg.AcquisitionUpgradeConcurrency)
}

func upgradePreviewMatcher(d Deps) ports.PreviewMatcher {
	cfg := d.Config
	printer := previewcheck.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := previewcheck.MustNew(d.NewClient(cfg.AcquisitionDeezerPreviewTimeout, d.Transport), previewcheck.BaseURL, printer, clock.Sleep, clock.Now)
	return previewcheck.NewMatcher(previews, printer, previewcheck.DefaultMatchMaxBitErrorRate)
}
