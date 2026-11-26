package downloadsetup

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/config"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

func upgradeLane(cfg *config.Config, pool *pgxpool.Pool, ledger download.TrackLedgerPort, svc *download.AcquireTrackAudioService) download.UpgradeLane {
	store := download.NewPostgresUpgradeStore(pool)
	runner := download.NewUpgradeRunner(ledger, svc, store,
		download.UpgradeConfig{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}).
		WithInPlaceCheck(svc, store, cfg.AcquisitionUpgradeVerifyPerHour)
	return download.UpgradeLaneOf(runner, cfg.AcquisitionUpgradeConcurrency)
}

func upgradePreviewMatcher(cfg *config.Config, transport http.RoundTripper) download.PreviewMatcher {
	printer := download.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := download.MustNewDeezerPreview(&http.Client{Timeout: cfg.AcquisitionDeezerPreviewTimeout, Transport: transport}, download.DeezerPreviewBaseURL, printer, clock.Sleep)
	return download.NewPreviewMatcher(previews, printer, download.DefaultMatchMaxBitErrorRate)
}
