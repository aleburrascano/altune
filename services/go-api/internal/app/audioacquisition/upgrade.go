package audioacquisition

import (
	"altune/go-api/internal/acquisition"
	"altune/go-api/internal/shared/binpath"
	"altune/go-api/internal/shared/config"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"
)

const deezerPreviewRate = 1.0

func upgradeLane(cfg *config.Config, pool *pgxpool.Pool, ledger acquisition.TrackLedgerPort, svc *acquisition.AcquireTrackAudioService) func(*acquisition.BackgroundAcquisitionScheduler) {
	store := acquisition.NewPgxUpgradeStore(pool)
	runner := acquisition.NewUpgradeRunner(ledger, svc, store,
		acquisition.UpgradeConfig{
			MaxTries:            cfg.AcquisitionUpgradeMaxTries,
			LowBitrateFloorKbps: cfg.AcquisitionLowBitrateFloorKbps,
		}).
		WithInPlaceCheck(svc, store, cfg.AcquisitionUpgradeVerifyPerHour)
	return acquisition.WithUpgradeLane(runner, cfg.AcquisitionUpgradeConcurrency)
}

func upgradePreviewMatcher(cfg *config.Config) acquisition.PreviewMatcher {
	printer := acquisition.NewFpcalcPrinter(binpath.Resolve("fpcalc", cfg.FFmpegLocation))
	previews := acquisition.NewDeezerPreview(&http.Client{Timeout: cfg.AcquisitionDeezerPreviewTimeout}, acquisition.DeezerPreviewBaseURL, deezerPreviewRate, printer)
	return acquisition.NewPreviewMatcher(previews, printer, acquisition.DefaultMatchMaxBitErrorRate)
}
